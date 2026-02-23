"""
Tests for TDTPClientDirect (D_* API).

Mirror of test_api_j.py — same logical scenarios, different client.
Additional coverage: memory management (free, context manager, double-free guard).
"""
from __future__ import annotations

import pytest

from tdtp import TDTPClientDirect, PacketHandle
from tdtp.exceptions import TDTPFilterError, TDTPParseError, TDTPProcessorError
from tests.conftest import (
    SAMPLE_FIELD_NAMES,
    SAMPLE_TOTAL_ROWS,
    SAMPLE_BALANCE_GT_1000_COUNT,
    SAMPLE_MOSCOW_COUNT,
)


# ---------------------------------------------------------------------------
# I/O
# ---------------------------------------------------------------------------

class TestDRead:
    def test_returns_packet_handle(self, d_client, sample_tdtp_path) -> None:
        handle = d_client.D_read(str(sample_tdtp_path))
        assert isinstance(handle, PacketHandle)
        handle.free()

    def test_schema_populated(self, d_client, sample_tdtp_path) -> None:
        with d_client.D_read_ctx(str(sample_tdtp_path)) as h:
            schema = h.get_schema()
            assert len(schema) == len(SAMPLE_FIELD_NAMES)
            names = [f["name"] for f in schema]
            assert names == SAMPLE_FIELD_NAMES

    def test_rows_populated(self, d_client, sample_tdtp_path) -> None:
        with d_client.D_read_ctx(str(sample_tdtp_path)) as h:
            rows = h.get_rows()
            assert len(rows) == SAMPLE_TOTAL_ROWS
            assert all(len(r) == len(SAMPLE_FIELD_NAMES) for r in rows)

    def test_nonexistent_file_raises(self, d_client) -> None:
        with pytest.raises(TDTPParseError):
            d_client.D_read("/no/such/file.tdtp")

    def test_context_manager_frees(self, d_client, sample_tdtp_path) -> None:
        with d_client.D_read_ctx(str(sample_tdtp_path)) as h:
            pass
        assert h._freed is True

    def test_double_free_is_idempotent(self, d_client, sample_tdtp_path) -> None:
        handle = d_client.D_read(str(sample_tdtp_path))
        handle.free()
        handle.free()  # must not raise or crash


class TestDWrite:
    def test_roundtrip_rows_equal(self, d_client, sample_tdtp_path, tmp_tdtp) -> None:
        with d_client.D_read_ctx(str(sample_tdtp_path)) as src:
            original_rows = src.get_rows()
            d_client.D_write(src, str(tmp_tdtp))

        with d_client.D_read_ctx(str(tmp_tdtp)) as re_read:
            assert re_read.get_rows() == original_rows


# ---------------------------------------------------------------------------
# TDTQL filtering
# ---------------------------------------------------------------------------

class TestDFilter:
    def test_no_filters_returns_all(self, d_client, sample_tdtp_path) -> None:
        with d_client.D_read_ctx(str(sample_tdtp_path)) as src:
            with d_client.D_filter(src, []) as out:
                assert len(out.get_rows()) == SAMPLE_TOTAL_ROWS

    def test_eq_filter(self, d_client, sample_tdtp_path) -> None:
        city_idx = SAMPLE_FIELD_NAMES.index("City")
        with d_client.D_read_ctx(str(sample_tdtp_path)) as src:
            with d_client.D_filter(src, [{"field": "City", "op": "eq", "value": "Moscow"}]) as out:
                rows = out.get_rows()
                assert len(rows) == SAMPLE_MOSCOW_COUNT
                assert all(r[city_idx] == "Moscow" for r in rows)

    def test_gt_filter(self, d_client, sample_tdtp_path) -> None:
        bal_idx = SAMPLE_FIELD_NAMES.index("Balance")
        with d_client.D_read_ctx(str(sample_tdtp_path)) as src:
            with d_client.D_filter(src, [{"field": "Balance", "op": "gt", "value": "1000"}]) as out:
                rows = out.get_rows()
                assert len(rows) == SAMPLE_BALANCE_GT_1000_COUNT
                assert all(float(r[bal_idx]) > 1000 for r in rows)

    def test_limit_respected(self, d_client, sample_tdtp_path) -> None:
        with d_client.D_read_ctx(str(sample_tdtp_path)) as src:
            with d_client.D_filter(src, [], limit=3) as out:
                assert len(out.get_rows()) == 3

    def test_result_packet_freed_independently(self, d_client, sample_tdtp_path) -> None:
        src = d_client.D_read(str(sample_tdtp_path))
        out = d_client.D_filter(src, [])
        # Free source first — out must still be valid.
        src.free()
        rows = out.get_rows()
        assert len(rows) == SAMPLE_TOTAL_ROWS
        out.free()

    def test_invalid_field_raises(self, d_client, sample_tdtp_path) -> None:
        with d_client.D_read_ctx(str(sample_tdtp_path)) as src:
            with pytest.raises(TDTPFilterError):
                result = d_client.D_filter(src, [{"field": "NoSuchField", "op": "eq", "value": "x"}])
                result.free()


# ---------------------------------------------------------------------------
# Processors
# ---------------------------------------------------------------------------

class TestDApplyMask:
    def test_masks_target_field(self, d_client, sample_tdtp_path) -> None:
        email_idx = SAMPLE_FIELD_NAMES.index("Email")
        with d_client.D_read_ctx(str(sample_tdtp_path)) as src:
            with d_client.D_apply_mask(src, ["Email"], visible_chars=0) as out:
                rows = out.get_rows()
                for row in rows:
                    assert "@" not in row[email_idx], f"email not masked: {row[email_idx]}"

    def test_non_target_fields_unchanged(self, d_client, sample_tdtp_path) -> None:
        name_idx = SAMPLE_FIELD_NAMES.index("Name")
        with d_client.D_read_ctx(str(sample_tdtp_path)) as src:
            original_names = [r[name_idx] for r in src.get_rows()]
            with d_client.D_apply_mask(src, ["Email"], visible_chars=0) as out:
                masked_names = [r[name_idx] for r in out.get_rows()]
                assert masked_names == original_names

    def test_empty_fields_list_passthrough(self, d_client, sample_tdtp_path) -> None:
        with d_client.D_read_ctx(str(sample_tdtp_path)) as src:
            original = src.get_rows()
            with d_client.D_apply_mask(src, [], visible_chars=0) as out:
                assert out.get_rows() == original

    def test_schema_preserved_after_mask(self, d_client, sample_tdtp_path) -> None:
        with d_client.D_read_ctx(str(sample_tdtp_path)) as src:
            orig_schema = src.get_schema()
            with d_client.D_apply_mask(src, ["Email"]) as out:
                assert out.get_schema() == orig_schema


class TestDCompressDecompress:
    def test_compress_produces_single_row(self, d_client, sample_tdtp_path) -> None:
        with d_client.D_read_ctx(str(sample_tdtp_path)) as src:
            with d_client.D_compress(src, level=1) as compressed:
                assert len(compressed.get_rows()) == 1

    def test_compression_flag_set(self, d_client, sample_tdtp_path) -> None:
        with d_client.D_read_ctx(str(sample_tdtp_path)) as src:
            with d_client.D_compress(src, level=1) as compressed:
                assert compressed.pkt.compression == b"zstd"

    def test_roundtrip_data_identical(self, d_client, sample_tdtp_path) -> None:
        with d_client.D_read_ctx(str(sample_tdtp_path)) as src:
            orig = src.get_rows()
            with d_client.D_compress(src) as c:
                with d_client.D_decompress(c) as d:
                    assert d.get_rows() == orig


# ---------------------------------------------------------------------------
# Memory safety
# ---------------------------------------------------------------------------

class TestMemorySafety:
    def test_use_after_free_raises(self, d_client, sample_tdtp_path) -> None:
        handle = d_client.D_read(str(sample_tdtp_path))
        handle.free()
        with pytest.raises(RuntimeError):
            handle.get_rows()

    def test_nested_free_order_independent(self, d_client, sample_tdtp_path) -> None:
        src = d_client.D_read(str(sample_tdtp_path))
        out = d_client.D_filter(src, [{"field": "Balance", "op": "gt", "value": "0"}])
        # Free out before src — both should work without corruption.
        out.free()
        src.free()
        assert src._freed and out._freed
