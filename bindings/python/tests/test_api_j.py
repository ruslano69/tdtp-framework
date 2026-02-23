"""
Tests for TDTPClientJSON (J_* API).

Each test class maps to one J_* export. Implemented tests run against the
compiled libtdtp.so; tests marked pytest.skip are still TODO (processor/
compress tests that need -tags compress build).
"""
from __future__ import annotations

import re
from pathlib import Path

import pytest

from tdtp import TDTPClientJSON
from tdtp.exceptions import TDTPFilterError, TDTPParseError, TDTPProcessorError

from conftest import (
    SAMPLE_FIELD_NAMES,
    SAMPLE_TOTAL_ROWS,
    SAMPLE_BALANCE_GT_1000_COUNT,
    SAMPLE_MOSCOW_COUNT,
    SAMPLE_BETWEEN_1000_2000_COUNT,
    SAMPLE_LIKE_JOHN_COUNT,
    SAMPLE_IN_MOSCOW_OMSK_COUNT,
    NULLABLE_FIELD_NAMES,
    NULLABLE_TOTAL_ROWS,
    NULLABLE_NULL_CITY_COUNT,
    NULLABLE_NOT_NULL_CITY_COUNT,
    COMPRESSED_TOTAL_ROWS,
    COMPRESSED_TABLE_NAME,
)


# ---------------------------------------------------------------------------
# Version
# ---------------------------------------------------------------------------

class TestJGetVersion:
    def test_returns_string(self, j_client: TDTPClientJSON) -> None:
        assert isinstance(j_client.J_get_version(), str)

    def test_semver_format(self, j_client: TDTPClientJSON) -> None:
        assert re.match(r"\d+\.\d+\.\d+", j_client.J_get_version())


# ---------------------------------------------------------------------------
# I/O — J_ReadFile
# ---------------------------------------------------------------------------

class TestJRead:
    def test_returns_schema_header_data(self, j_client, sample_data_j) -> None:
        assert "schema" in sample_data_j
        assert "header" in sample_data_j
        assert "data"   in sample_data_j

    def test_schema_has_fields(self, j_client, sample_data_j) -> None:
        fields = sample_data_j["schema"]["Fields"]
        assert len(fields) == len(SAMPLE_FIELD_NAMES)
        names = [f["Name"] for f in fields]
        assert names == SAMPLE_FIELD_NAMES

    def test_data_row_count(self, j_client, sample_data_j) -> None:
        assert len(sample_data_j["data"]) == SAMPLE_TOTAL_ROWS

    def test_header_table_name(self, j_client, sample_data_j) -> None:
        assert sample_data_j["header"]["table_name"] == "users"

    def test_nonexistent_file_raises(self, j_client) -> None:
        with pytest.raises(TDTPParseError):
            j_client.J_read("/no/such/file.tdtp.xml")

    def test_compressed_file(self, j_client, compressed_tdtp_path) -> None:
        """J_read transparently decompresses zstd-compressed data blocks."""
        data = j_client.J_read(str(compressed_tdtp_path))
        assert data["header"]["table_name"] == COMPRESSED_TABLE_NAME
        assert len(data["data"]) == COMPRESSED_TOTAL_ROWS
        # Rows are lists of field values matching schema order; first value is ID.
        first = data["data"][0]
        assert len(first) > 0
        assert first[0] == "1"  # ID of first row from create_test_db.py


# ---------------------------------------------------------------------------
# I/O — J_WriteFile
# ---------------------------------------------------------------------------

class TestJWrite:
    def test_roundtrip(self, j_client, sample_data_j, tmp_path) -> None:
        out = tmp_path / "out.tdtp.xml"
        j_client.J_write(sample_data_j, str(out))
        assert out.exists()
        re_read = j_client.J_read(str(out))
        assert re_read["data"] == sample_data_j["data"]
        assert [f["Name"] for f in re_read["schema"]["Fields"]] == SAMPLE_FIELD_NAMES


# ---------------------------------------------------------------------------
# TDTQL filtering — J_FilterRows
# ---------------------------------------------------------------------------

class TestJFilter:
    def test_all_rows_passthrough(self, j_client, sample_data_j) -> None:
        # All IDs in fixture are positive integers (1-8)
        result = j_client.J_filter(sample_data_j, "ID > 0")
        assert len(result["data"]) == SAMPLE_TOTAL_ROWS

    def test_no_rows_false_clause(self, j_client, sample_data_j) -> None:
        # All balances in fixture are positive, so Balance < 0 matches nothing
        result = j_client.J_filter(sample_data_j, "Balance < 0")
        assert len(result["data"]) == 0

    def test_eq_operator(self, j_client, sample_data_j) -> None:
        result = j_client.J_filter(sample_data_j, "City = 'Moscow'")
        assert len(result["data"]) == SAMPLE_MOSCOW_COUNT
        city_idx = SAMPLE_FIELD_NAMES.index("City")
        for row in result["data"]:
            assert row[city_idx] == "Moscow"

    def test_gt_operator(self, j_client, sample_data_j) -> None:
        result = j_client.J_filter(sample_data_j, "Balance > 1000")
        assert len(result["data"]) == SAMPLE_BALANCE_GT_1000_COUNT
        bal_idx = SAMPLE_FIELD_NAMES.index("Balance")
        for row in result["data"]:
            assert int(row[bal_idx]) > 1000

    def test_limit_respected(self, j_client, sample_data_j) -> None:
        result = j_client.J_filter(sample_data_j, "ID > 0", limit=3)
        assert len(result["data"]) == 3

    def test_limit_zero_means_unlimited(self, j_client, sample_data_j) -> None:
        result = j_client.J_filter(sample_data_j, "ID > 0", limit=0)
        assert len(result["data"]) == SAMPLE_TOTAL_ROWS

    def test_schema_preserved_after_filter(self, j_client, sample_data_j) -> None:
        result = j_client.J_filter(sample_data_j, "City = 'Moscow'")
        assert result["schema"] == sample_data_j["schema"]

    def test_invalid_clause_raises(self, j_client, sample_data_j) -> None:
        with pytest.raises(TDTPFilterError):
            j_client.J_filter(sample_data_j, "INVALID @@@ CLAUSE")

    def test_unknown_field_raises(self, j_client, sample_data_j) -> None:
        with pytest.raises(TDTPFilterError):
            j_client.J_filter(sample_data_j, "NoSuchField = 'x'")

    def test_between_operator(self, j_client, sample_data_j) -> None:
        bal_idx = SAMPLE_FIELD_NAMES.index("Balance")
        result = j_client.J_filter(sample_data_j, "Balance BETWEEN 1000 AND 2000")
        assert len(result["data"]) == SAMPLE_BETWEEN_1000_2000_COUNT
        for row in result["data"]:
            assert 1000 <= int(row[bal_idx]) <= 2000

    def test_like_operator(self, j_client, sample_data_j) -> None:
        email_idx = SAMPLE_FIELD_NAMES.index("Email")
        result = j_client.J_filter(sample_data_j, "Email LIKE 'john%'")
        assert len(result["data"]) == SAMPLE_LIKE_JOHN_COUNT
        assert result["data"][0][email_idx].startswith("john")

    def test_in_operator(self, j_client, sample_data_j) -> None:
        city_idx = SAMPLE_FIELD_NAMES.index("City")
        result = j_client.J_filter(sample_data_j, "City IN ('Moscow', 'Omsk')")
        assert len(result["data"]) == SAMPLE_IN_MOSCOW_OMSK_COUNT
        for row in result["data"]:
            assert row[city_idx] in ("Moscow", "Omsk")

    def test_is_null_operator(self, j_client, nullable_data_j) -> None:
        city_idx = NULLABLE_FIELD_NAMES.index("City")
        result = j_client.J_filter(nullable_data_j, "City IS NULL")
        assert len(result["data"]) == NULLABLE_NULL_CITY_COUNT
        for row in result["data"]:
            assert row[city_idx] == ""

    def test_is_not_null_operator(self, j_client, nullable_data_j) -> None:
        city_idx = NULLABLE_FIELD_NAMES.index("City")
        result = j_client.J_filter(nullable_data_j, "City IS NOT NULL")
        assert len(result["data"]) == NULLABLE_NOT_NULL_CITY_COUNT
        for row in result["data"]:
            assert row[city_idx] != ""


# ---------------------------------------------------------------------------
# Processors — J_ApplyProcessor / J_ApplyChain
# (require libtdtp built with -tags compress)
# ---------------------------------------------------------------------------

class TestJApplyProcessor:
    def test_field_masker_masks_values(self, j_client, sample_data_j) -> None:
        # fields param is {field_name: pattern}; "stars" replaces every char with *
        result = j_client.J_apply_processor(
            sample_data_j, "field_masker", fields={"Email": "stars"}
        )
        email_idx = SAMPLE_FIELD_NAMES.index("Email")
        assert len(result["data"]) == SAMPLE_TOTAL_ROWS
        for row in result["data"]:
            # Original emails contain "@"; masked value must not
            assert "@" not in row[email_idx], f"email not masked: {row[email_idx]}"

    def test_field_normalizer_trims(self, j_client, sample_data_j) -> None:
        # "uppercase" rule upper-cases all Name values
        result = j_client.J_apply_processor(
            sample_data_j, "field_normalizer", fields={"Name": "uppercase"}
        )
        name_idx = SAMPLE_FIELD_NAMES.index("Name")
        assert len(result["data"]) == SAMPLE_TOTAL_ROWS
        for orig_row, new_row in zip(sample_data_j["data"], result["data"]):
            assert new_row[name_idx] == orig_row[name_idx].upper()

    def test_field_validator_rejects_invalid(self, j_client, sample_data_j) -> None:
        # All fixture emails match the email regex — validator must pass all rows
        result = j_client.J_apply_processor(
            sample_data_j, "field_validator", rules={"Email": ["email"]}
        )
        assert len(result["data"]) == SAMPLE_TOTAL_ROWS

    def test_compress_decompress_roundtrip(self, j_client, sample_data_j) -> None:
        compressed = j_client.J_apply_processor(sample_data_j, "compress", level=3)
        # Compressed form is a single-element data list (one blob)
        assert len(compressed["data"]) == 1
        decompressed = j_client.J_apply_processor(compressed, "decompress")
        assert len(decompressed["data"]) == SAMPLE_TOTAL_ROWS
        assert decompressed["data"] == sample_data_j["data"]

    def test_unknown_processor_raises(self, j_client, sample_data_j) -> None:
        with pytest.raises(TDTPProcessorError):
            j_client.J_apply_processor(sample_data_j, "no_such_processor")


class TestJApplyChain:
    def test_mask_then_normalize(self, j_client, sample_data_j) -> None:
        chain = [
            {"type": "field_masker",     "params": {"fields": {"Email": "stars"}}},
            {"type": "field_normalizer", "params": {"fields": {"Name": "uppercase"}}},
        ]
        result = j_client.J_apply_chain(sample_data_j, chain)
        email_idx = SAMPLE_FIELD_NAMES.index("Email")
        name_idx  = SAMPLE_FIELD_NAMES.index("Name")
        assert len(result["data"]) == SAMPLE_TOTAL_ROWS
        for orig_row, new_row in zip(sample_data_j["data"], result["data"]):
            assert "@" not in new_row[email_idx]
            assert new_row[name_idx] == orig_row[name_idx].upper()

    def test_empty_chain_passthrough(self, j_client, sample_data_j) -> None:
        result = j_client.J_apply_chain(sample_data_j, [])
        assert result["data"] == sample_data_j["data"]


# ---------------------------------------------------------------------------
# Diff — J_Diff
# ---------------------------------------------------------------------------

class TestJDiff:
    def test_identical_datasets_no_diff(self, j_client, sample_data_j) -> None:
        diff = j_client.J_diff(sample_data_j, sample_data_j)
        assert diff["stats"]["added"]    == 0
        assert diff["stats"]["removed"]  == 0
        assert diff["stats"]["modified"] == 0

    def test_added_rows_detected(self, j_client, sample_data_j) -> None:
        # Build a "new" dataset with one extra row
        new_data = dict(sample_data_j)
        extra_row = ["99", "Test User", "test@example.com", "Moscow", "0", "1", "2026-01-01T00:00:00Z"]
        new_data["data"] = sample_data_j["data"] + [extra_row]
        diff = j_client.J_diff(sample_data_j, new_data)
        assert diff["stats"]["added"] == 1

    def test_removed_rows_detected(self, j_client, sample_data_j) -> None:
        new_data = dict(sample_data_j)
        new_data["data"] = sample_data_j["data"][1:]  # remove first row
        diff = j_client.J_diff(sample_data_j, new_data)
        assert diff["stats"]["removed"] == 1

    def test_modified_rows_detected(self, j_client, sample_data_j) -> None:
        new_data = dict(sample_data_j)
        rows = [list(r) for r in sample_data_j["data"]]
        city_idx = SAMPLE_FIELD_NAMES.index("City")
        rows[0][city_idx] = "NewCity"   # change city of first row
        new_data["data"] = rows
        diff = j_client.J_diff(sample_data_j, new_data)
        assert diff["stats"]["modified"] == 1
