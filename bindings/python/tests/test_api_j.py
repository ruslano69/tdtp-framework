"""
Tests for TDTPClientJSON (J_* API).

Each test function maps to one J_* export. Tests are organized from
simplest (J_get_version) to most complex (J_apply_chain, J_diff).
"""
from __future__ import annotations

from pathlib import Path

import pytest

from tdtp import TDTPClientJSON
from tdtp.exceptions import TDTPFilterError, TDTPParseError, TDTPProcessorError


# ---------------------------------------------------------------------------
# Version
# ---------------------------------------------------------------------------

class TestJGetVersion:
    def test_returns_string(self, j_client: TDTPClientJSON) -> None:
        # TODO: assert isinstance(j_client.J_get_version(), str)
        pytest.skip("TODO: implement J_GetVersion")

    def test_semver_format(self, j_client: TDTPClientJSON) -> None:
        # TODO: assert re.match(r"\d+\.\d+\.\d+", j_client.J_get_version())
        pytest.skip("TODO: implement J_GetVersion")


# ---------------------------------------------------------------------------
# I/O
# ---------------------------------------------------------------------------

class TestJRead:
    def test_returns_schema_header_data(self, j_client, sample_tdtp_path) -> None:
        # TODO: data = j_client.J_read(str(sample_tdtp_path))
        # TODO: assert "schema" in data and "header" in data and "data" in data
        pytest.skip("TODO: implement J_ReadFile")

    def test_schema_has_fields(self, j_client, sample_tdtp_path) -> None:
        # TODO: data = j_client.J_read(str(sample_tdtp_path))
        # TODO: assert len(data["schema"]["fields"]) > 0
        pytest.skip("TODO: implement J_ReadFile")

    def test_nonexistent_file_raises(self, j_client) -> None:
        # TODO: with pytest.raises(TDTPParseError): j_client.J_read("/no/such/file.tdtp")
        pytest.skip("TODO: implement J_ReadFile")

    def test_compressed_file(self, j_client, tmp_dir) -> None:
        # TODO: write a zstd-compressed .tdtp fixture, read it, check rows
        pytest.skip("TODO: add compressed fixture")


class TestJWrite:
    def test_roundtrip(self, j_client, sample_data_j, tmp_dir) -> None:
        # TODO: out = tmp_dir / "out.tdtp"
        # TODO: j_client.J_write(sample_data_j, str(out))
        # TODO: re_read = j_client.J_read(str(out))
        # TODO: assert re_read["data"] == sample_data_j["data"]
        pytest.skip("TODO: implement J_WriteFile")


# ---------------------------------------------------------------------------
# TDTQL filtering
# ---------------------------------------------------------------------------

class TestJFilter:
    @pytest.mark.parametrize("where,expected_min", [
        ("1 = 1",          1),    # always true — all rows
        ("1 = 0",          0),    # always false — no rows
    ])
    def test_trivial_clauses(self, j_client, sample_data_j, where, expected_min) -> None:
        # TODO: result = j_client.J_filter(sample_data_j, where)
        # TODO: assert len(result["data"]) >= expected_min
        pytest.skip("TODO: implement J_FilterRows")

    def test_eq_operator(self, j_client, sample_data_j) -> None:
        # TODO: pick a known field/value from the sample, filter, verify rows
        pytest.skip("TODO: implement J_FilterRows")

    def test_gt_operator(self, j_client, sample_data_j) -> None:
        pytest.skip("TODO: implement J_FilterRows")

    def test_between_operator(self, j_client, sample_data_j) -> None:
        pytest.skip("TODO: implement J_FilterRows")

    def test_like_operator(self, j_client, sample_data_j) -> None:
        pytest.skip("TODO: implement J_FilterRows")

    def test_is_null_operator(self, j_client, sample_data_j) -> None:
        pytest.skip("TODO: implement J_FilterRows")

    def test_limit_respected(self, j_client, sample_data_j) -> None:
        # TODO: result = j_client.J_filter(sample_data_j, "1=1", limit=2)
        # TODO: assert len(result["data"]) <= 2
        pytest.skip("TODO: implement J_FilterRows")

    def test_invalid_clause_raises(self, j_client, sample_data_j) -> None:
        # TODO: with pytest.raises(TDTPFilterError):
        #           j_client.J_filter(sample_data_j, "INVALID @@@ CLAUSE")
        pytest.skip("TODO: implement J_FilterRows")

    def test_unknown_field_raises(self, j_client, sample_data_j) -> None:
        # TODO: with pytest.raises(TDTPFilterError):
        #           j_client.J_filter(sample_data_j, "NoSuchField = 'x'")
        pytest.skip("TODO: implement J_FilterRows")

    def test_schema_preserved(self, j_client, sample_data_j) -> None:
        # TODO: result = j_client.J_filter(sample_data_j, "1=1")
        # TODO: assert result["schema"] == sample_data_j["schema"]
        pytest.skip("TODO: implement J_FilterRows")


# ---------------------------------------------------------------------------
# Processors
# ---------------------------------------------------------------------------

class TestJApplyProcessor:
    def test_field_masker_masks_values(self, j_client, sample_data_j) -> None:
        # TODO: pick a string field
        # TODO: result = j_client.J_apply_processor(data, "field_masker",
        #                   fields=[field], mask_char="*", visible_chars=0)
        # TODO: assert all cells in that column are "*" * len(original)
        pytest.skip("TODO: implement J_ApplyProcessor + field_masker")

    def test_field_normalizer_trims(self, j_client, sample_data_j) -> None:
        pytest.skip("TODO: implement J_ApplyProcessor + field_normalizer")

    def test_field_validator_rejects_invalid(self, j_client, sample_data_j) -> None:
        pytest.skip("TODO: implement J_ApplyProcessor + field_validator")

    def test_compress_decompress_roundtrip(self, j_client, sample_data_j) -> None:
        # TODO: compressed   = j_client.J_apply_processor(data, "compress", level=3)
        # TODO: decompressed = j_client.J_apply_processor(compressed, "decompress")
        # TODO: assert decompressed["data"] == sample_data_j["data"]
        pytest.skip("TODO: implement J_ApplyProcessor + compress/decompress")

    def test_unknown_processor_raises(self, j_client, sample_data_j) -> None:
        # TODO: with pytest.raises(TDTPProcessorError):
        #           j_client.J_apply_processor(data, "nonexistent_processor")
        pytest.skip("TODO: implement J_ApplyProcessor")


class TestJApplyChain:
    def test_mask_then_compress(self, j_client, sample_data_j) -> None:
        # TODO: chain = [{"type": "field_masker", "params": {"fields": [...]}},
        #                {"type": "compress",     "params": {"level": 1}}]
        # TODO: result = j_client.J_apply_chain(data, chain)
        # TODO: assert result["data"] is not None (non-empty compressed blob)
        pytest.skip("TODO: implement J_ApplyChain")

    def test_empty_chain_passthrough(self, j_client, sample_data_j) -> None:
        # TODO: result = j_client.J_apply_chain(data, [])
        # TODO: assert result["data"] == sample_data_j["data"]
        pytest.skip("TODO: implement J_ApplyChain")


# ---------------------------------------------------------------------------
# Diff
# ---------------------------------------------------------------------------

class TestJDiff:
    def test_identical_datasets_no_diff(self, j_client, sample_data_j) -> None:
        # TODO: diff = j_client.J_diff(sample_data_j, sample_data_j)
        # TODO: assert diff["stats"] == {"added": 0, "removed": 0, "modified": 0}
        pytest.skip("TODO: implement J_Diff")

    def test_added_rows_detected(self, j_client, sample_data_j) -> None:
        pytest.skip("TODO: implement J_Diff")

    def test_removed_rows_detected(self, j_client, sample_data_j) -> None:
        pytest.skip("TODO: implement J_Diff")

    def test_modified_rows_detected(self, j_client, sample_data_j) -> None:
        pytest.skip("TODO: implement J_Diff")
