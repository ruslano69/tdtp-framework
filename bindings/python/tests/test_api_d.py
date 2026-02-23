"""
Tests for TDTPClientDirect (D_* API).

Mirror of test_api_j.py — same logical scenarios, different client.
Additional coverage: memory management (free, context manager, double-free guard).
"""
from __future__ import annotations

from pathlib import Path

import pytest

from tdtp import TDTPClientDirect, PacketHandle
from tdtp.exceptions import TDTPFilterError, TDTPParseError, TDTPProcessorError


# ---------------------------------------------------------------------------
# I/O
# ---------------------------------------------------------------------------

class TestDRead:
    def test_returns_packet_handle(self, d_client, sample_tdtp_path) -> None:
        # TODO: handle = d_client.D_read(str(sample_tdtp_path))
        # TODO: assert isinstance(handle, PacketHandle)
        # TODO: handle.free()
        pytest.skip("TODO: implement D_ReadFile")

    def test_schema_populated(self, d_client, sample_tdtp_path) -> None:
        # TODO: with d_client.D_read_ctx(str(sample_tdtp_path)) as pkt:
        #           assert len(pkt.get_schema()) > 0
        pytest.skip("TODO: implement D_ReadFile")

    def test_rows_populated(self, d_client, sample_tdtp_path) -> None:
        # TODO: with d_client.D_read_ctx(str(sample_tdtp_path)) as pkt:
        #           assert len(pkt.get_rows()) > 0
        pytest.skip("TODO: implement D_ReadFile")

    def test_nonexistent_file_raises(self, d_client) -> None:
        # TODO: with pytest.raises(TDTPParseError):
        #           d_client.D_read("/no/such/file.tdtp")
        pytest.skip("TODO: implement D_ReadFile")

    def test_context_manager_frees(self, d_client, sample_tdtp_path) -> None:
        # TODO: with d_client.D_read_ctx(str(sample_tdtp_path)) as h:
        #           pass
        # TODO: assert h._freed is True
        pytest.skip("TODO: implement D_ReadFile")

    def test_double_free_raises(self, d_client, sample_tdtp_path) -> None:
        # TODO: handle = d_client.D_read(str(sample_tdtp_path))
        # TODO: handle.free()
        # TODO: handle.free()  # should not crash — idempotent
        pytest.skip("TODO: implement D_ReadFile")


class TestDWrite:
    def test_roundtrip_rows_equal(self, d_client, sample_tdtp_path, tmp_dir) -> None:
        # TODO: with d_client.D_read_ctx(str(sample_tdtp_path)) as src:
        #           original_rows = src.get_rows()
        #           out = tmp_dir / "out.tdtp"
        #           d_client.D_write(src, str(out))
        # TODO: with d_client.D_read_ctx(str(out)) as re_read:
        #           assert re_read.get_rows() == original_rows
        pytest.skip("TODO: implement D_WriteFile")


# ---------------------------------------------------------------------------
# TDTQL filtering
# ---------------------------------------------------------------------------

class TestDFilter:
    def test_no_filters_returns_all(self, d_client, sample_tdtp_path) -> None:
        # TODO: with d_client.D_read_ctx(str(sample_tdtp_path)) as src:
        #           with d_client.D_filter(src, [], limit=0) as out:
        #               assert len(out.get_rows()) == len(src.get_rows())
        pytest.skip("TODO: implement D_FilterRows")

    def test_eq_filter(self, d_client, sample_tdtp_path) -> None:
        # TODO: filters = [{"field": "...", "op": "eq", "value": "..."}]
        # TODO: with d_client.D_read_ctx(...) as src:
        #           with d_client.D_filter(src, filters) as out:
        #               assert all rows satisfy condition
        pytest.skip("TODO: implement D_FilterRows")

    def test_limit_respected(self, d_client, sample_tdtp_path) -> None:
        # TODO: with d_client.D_read_ctx(...) as src:
        #           with d_client.D_filter(src, [], limit=1) as out:
        #               assert len(out.get_rows()) <= 1
        pytest.skip("TODO: implement D_FilterRows")

    def test_result_packet_freed_independently(self, d_client, sample_tdtp_path) -> None:
        # TODO: ensure freeing src does not corrupt out and vice versa
        pytest.skip("TODO: implement D_FilterRows")


# ---------------------------------------------------------------------------
# Processors
# ---------------------------------------------------------------------------

class TestDApplyMask:
    def test_masks_target_field(self, d_client, sample_tdtp_path) -> None:
        # TODO: with d_client.D_read_ctx(...) as src:
        #           with d_client.D_apply_mask(src, [field], visible_chars=0) as out:
        #               for row in out.get_rows():
        #                   assert all(c == "*" for c in row[field_idx])
        pytest.skip("TODO: implement D_ApplyMask")

    def test_non_target_fields_unchanged(self, d_client, sample_tdtp_path) -> None:
        pytest.skip("TODO: implement D_ApplyMask")

    def test_empty_fields_list_passthrough(self, d_client, sample_tdtp_path) -> None:
        pytest.skip("TODO: implement D_ApplyMask")


class TestDCompressDecompress:
    def test_compress_changes_compression_flag(self, d_client, sample_tdtp_path) -> None:
        # TODO: with d_client.D_read_ctx(...) as src:
        #           with d_client.D_compress(src, level=1) as compressed:
        #               assert compressed.pkt.compression == b"zstd"
        pytest.skip("TODO: implement D_ApplyCompress")

    def test_roundtrip_data_identical(self, d_client, sample_tdtp_path) -> None:
        # TODO: with d_client.D_read_ctx(...) as src:
        #           orig = src.get_rows()
        #           with d_client.D_compress(src) as c:
        #               with d_client.D_decompress(c) as d:
        #                   assert d.get_rows() == orig
        pytest.skip("TODO: implement D_ApplyCompress + D_ApplyDecompress")


# ---------------------------------------------------------------------------
# Memory safety
# ---------------------------------------------------------------------------

class TestMemorySafety:
    def test_use_after_free_raises(self, d_client, sample_tdtp_path) -> None:
        # TODO: handle = d_client.D_read(str(sample_tdtp_path))
        # TODO: handle.free()
        # TODO: with pytest.raises(RuntimeError): handle.get_rows()
        pytest.skip("TODO: implement PacketHandle.free guard")

    def test_nested_free_order_independent(self, d_client, sample_tdtp_path) -> None:
        # TODO: verify that freeing src before out doesn't corrupt out
        pytest.skip("TODO: memory model validation")
