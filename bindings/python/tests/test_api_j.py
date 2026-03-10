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
    COMPACT_TOTAL_ROWS,
    COMPACT_TABLE_NAME,
    COMPACT_FIELD_NAMES,
    COMPACT_SALES_COUNT,
    COMPACT_IT_COUNT,
    COMPACT_HR_COUNT,
    COMPACT_SALARY_GT_60000,
    COMPACT_SALARY_GE_60000,
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
# Pagination — offset + query_context (J_FilterRowsPage under the hood)
# ---------------------------------------------------------------------------

class TestJFilterPagination:
    def test_offset_skips_rows(self, j_client, sample_data_j) -> None:
        all_rows = j_client.J_filter(sample_data_j, "ID > 0")["data"]
        page     = j_client.J_filter(sample_data_j, "ID > 0", limit=3, offset=2)
        assert page["data"] == all_rows[2:5]

    def test_query_context_present_with_limit(self, j_client, sample_data_j) -> None:
        result = j_client.J_filter(sample_data_j, "ID > 0", limit=3)
        assert "query_context" in result

    def test_query_context_total_records(self, j_client, sample_data_j) -> None:
        result = j_client.J_filter(sample_data_j, "ID > 0", limit=3)
        assert result["query_context"]["total_records"] == SAMPLE_TOTAL_ROWS

    def test_query_context_matched_records(self, j_client, sample_data_j) -> None:
        result = j_client.J_filter(sample_data_j, "Balance > 1000", limit=2)
        assert result["query_context"]["matched_records"] == SAMPLE_BALANCE_GT_1000_COUNT

    def test_query_context_returned_records(self, j_client, sample_data_j) -> None:
        result = j_client.J_filter(sample_data_j, "ID > 0", limit=3)
        assert result["query_context"]["returned_records"] == 3
        assert len(result["data"]) == 3

    def test_more_available_true_on_partial_page(self, j_client, sample_data_j) -> None:
        result = j_client.J_filter(sample_data_j, "ID > 0", limit=3)
        assert result["query_context"]["more_available"] is True

    def test_more_available_false_on_last_page(self, j_client, sample_data_j) -> None:
        result = j_client.J_filter(sample_data_j, "ID > 0", limit=3, offset=6)
        assert result["query_context"]["more_available"] is False

    def test_next_offset_points_to_next_page(self, j_client, sample_data_j) -> None:
        result = j_client.J_filter(sample_data_j, "ID > 0", limit=3)
        assert result["query_context"]["next_offset"] == 3

    def test_walk_all_pages(self, j_client, sample_data_j) -> None:
        """Paginate through all rows and verify completeness."""
        page_size = 3
        collected = []
        offset    = 0
        while True:
            page = j_client.J_filter(sample_data_j, "ID > 0", limit=page_size, offset=offset)
            collected.extend(page["data"])
            qc = page["query_context"]
            if not qc["more_available"]:
                break
            offset = qc["next_offset"]
        all_rows = j_client.J_filter(sample_data_j, "ID > 0")["data"]
        assert collected == all_rows

    def test_offset_without_limit_returns_tail(self, j_client, sample_data_j) -> None:
        all_rows = j_client.J_filter(sample_data_j, "ID > 0")["data"]
        result   = j_client.J_filter(sample_data_j, "ID > 0", offset=5)
        assert result["data"] == all_rows[5:]

    def test_schema_preserved_with_offset(self, j_client, sample_data_j) -> None:
        result = j_client.J_filter(sample_data_j, "ID > 0", limit=2, offset=1)
        assert result["schema"] == sample_data_j["schema"]


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


# ---------------------------------------------------------------------------
# Compact v1.3.1 — carry-forward fixed fields must be expanded on read
# ---------------------------------------------------------------------------

class TestJReadCompact:
    """J_ReadFile must expand compact carry-forward encoding before returning rows."""

    def test_row_count(self, j_client, compact_data_j) -> None:
        assert len(compact_data_j["data"]) == COMPACT_TOTAL_ROWS

    def test_table_name(self, j_client, compact_data_j) -> None:
        assert compact_data_j["header"]["table_name"] == COMPACT_TABLE_NAME

    def test_schema_field_names(self, j_client, compact_data_j) -> None:
        names = [f["Name"] for f in compact_data_j["schema"]["Fields"]]
        assert names == COMPACT_FIELD_NAMES

    def test_first_row_fully_populated(self, j_client, compact_data_j) -> None:
        """First row of the first group must have all fields."""
        row = compact_data_j["data"][0]
        assert row[0] == "D1"
        assert row[1] == "Sales"
        assert row[2] == "Moscow"

    def test_carry_forward_rows_expanded(self, j_client, compact_data_j) -> None:
        """Rows 2 and 3 carry over DeptID/DeptName/Location from row 1."""
        for row in compact_data_j["data"][1:3]:
            assert row[0] == "D1",    f"expected DeptID='D1', got '{row[0]}'"
            assert row[1] == "Sales", f"expected DeptName='Sales', got '{row[1]}'"
            assert row[2] == "Moscow", f"expected Location='Moscow', got '{row[2]}'"

    def test_group_boundary_expanded(self, j_client, compact_data_j) -> None:
        """Row 4 starts a new group (D2/IT/SPb) and must be fully populated."""
        row = compact_data_j["data"][3]
        assert row[0] == "D2"
        assert row[1] == "IT"
        assert row[2] == "SPb"

    def test_second_group_carry_forward_expanded(self, j_client, compact_data_j) -> None:
        """Row 5 carries over D2/IT/SPb from row 4."""
        row = compact_data_j["data"][4]
        assert row[0] == "D2"
        assert row[1] == "IT"
        assert row[2] == "SPb"

    def test_last_group_single_row(self, j_client, compact_data_j) -> None:
        """Row 6 is a single-row group (D3/HR/Kazan)."""
        row = compact_data_j["data"][5]
        assert row[0] == "D3"
        assert row[1] == "HR"
        assert row[2] == "Kazan"

    def test_variable_fields_preserved(self, j_client, compact_data_j) -> None:
        """Variable fields (EmpID, Name, Salary) must not be affected by expansion."""
        emp_ids = [r[3] for r in compact_data_j["data"]]
        assert emp_ids == ["E1", "E2", "E3", "E4", "E5", "E6"]


class TestJCompactPagination:
    """Pagination on compact-read data: every page must have fully populated fixed fields."""

    def test_filter_by_fixed_field_all_rows(self, j_client, compact_data_j) -> None:
        """Filter on fixed field must match carry-forward rows, not just first row."""
        result = j_client.J_filter(compact_data_j, "DeptName = 'Sales'")
        assert len(result["data"]) == COMPACT_SALES_COUNT

    def test_filter_it_dept(self, j_client, compact_data_j) -> None:
        result = j_client.J_filter(compact_data_j, "DeptName = 'IT'")
        assert len(result["data"]) == COMPACT_IT_COUNT

    def test_filter_hr_dept(self, j_client, compact_data_j) -> None:
        result = j_client.J_filter(compact_data_j, "DeptName = 'HR'")
        assert len(result["data"]) == COMPACT_HR_COUNT

    def test_filter_salary_gt(self, j_client, compact_data_j) -> None:
        result = j_client.J_filter(compact_data_j, "Salary > 60000")
        assert len(result["data"]) == COMPACT_SALARY_GT_60000

    def test_filter_salary_gte(self, j_client, compact_data_j) -> None:
        result = j_client.J_filter(compact_data_j, "Salary >= 60000")
        assert len(result["data"]) == COMPACT_SALARY_GE_60000

    def test_page1_fixed_fields_populated(self, j_client, compact_data_j) -> None:
        """Page 1 (rows 0-1): both rows must have DeptID filled."""
        page = j_client.J_filter(compact_data_j, "EmpID > ''", limit=2, offset=0)
        dept_idx = COMPACT_FIELD_NAMES.index("DeptID")
        for row in page["data"]:
            assert row[dept_idx] != "", f"DeptID empty on page1: {row}"

    def test_page2_fixed_fields_populated(self, j_client, compact_data_j) -> None:
        """Page 2 (rows 2-3): crosses group boundary — all fixed fields must be non-empty."""
        page = j_client.J_filter(compact_data_j, "EmpID > ''", limit=2, offset=2)
        dept_idx  = COMPACT_FIELD_NAMES.index("DeptID")
        dname_idx = COMPACT_FIELD_NAMES.index("DeptName")
        for row in page["data"]:
            assert row[dept_idx]  != "", f"DeptID empty on page2: {row}"
            assert row[dname_idx] != "", f"DeptName empty on page2: {row}"

    def test_page3_fixed_fields_populated(self, j_client, compact_data_j) -> None:
        """Page 3 (rows 4-5): second group carry-forward + single-row last group."""
        page = j_client.J_filter(compact_data_j, "EmpID > ''", limit=2, offset=4)
        dept_idx = COMPACT_FIELD_NAMES.index("DeptID")
        for row in page["data"]:
            assert row[dept_idx] != "", f"DeptID empty on page3: {row}"

    def test_walk_all_pages_fixed_fields_complete(self, j_client, compact_data_j) -> None:
        """Walk all pages; every collected row must have non-empty DeptID."""
        dept_idx = COMPACT_FIELD_NAMES.index("DeptID")
        page_size = 2
        collected = []
        offset = 0
        while True:
            page = j_client.J_filter(compact_data_j, "EmpID > ''", limit=page_size, offset=offset)
            collected.extend(page["data"])
            qc = page["query_context"]
            if not qc["more_available"]:
                break
            offset = qc["next_offset"]
        assert len(collected) == COMPACT_TOTAL_ROWS
        for row in collected:
            assert row[dept_idx] != "", f"DeptID empty in walked row: {row}"

    def test_cross_group_boundary_correct_dept(self, j_client, compact_data_j) -> None:
        """Page crossing group boundary must have correct (different) dept values."""
        page = j_client.J_filter(compact_data_j, "EmpID > ''", limit=2, offset=2)
        dname_idx = COMPACT_FIELD_NAMES.index("DeptName")
        # row at offset 2 = E3/Carol → Sales; row at offset 3 = E4/Dave → IT
        assert page["data"][0][dname_idx] == "Sales"
        assert page["data"][1][dname_idx] == "IT"
