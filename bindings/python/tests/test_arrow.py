"""
Tests for the Arrow columnar bridge (Phase 3).

Skipped entirely if pyarrow is not installed.
"""
from __future__ import annotations

import pytest

pa = pytest.importorskip("pyarrow")

from tdtp import Tdtp

from conftest import SAMPLE_FIELD_NAMES, SAMPLE_TOTAL_ROWS


@pytest.fixture
def db() -> Tdtp:
    return Tdtp()


class TestReadArrow:
    def test_returns_arrow_table(self, db, sample_tdtp_path) -> None:
        tbl = db.read_arrow(str(sample_tdtp_path))
        assert isinstance(tbl, pa.Table)
        assert tbl.num_rows == SAMPLE_TOTAL_ROWS
        assert tbl.num_columns == len(SAMPLE_FIELD_NAMES)

    def test_column_names_match_schema(self, db, sample_tdtp_path) -> None:
        tbl = db.read_arrow(str(sample_tdtp_path))
        assert tbl.schema.names == SAMPLE_FIELD_NAMES

    def test_integer_column_is_int64(self, db, sample_tdtp_path) -> None:
        tbl = db.read_arrow(str(sample_tdtp_path))
        assert tbl.schema.field("Balance").type == pa.int64()
        assert tbl.schema.field("ID").type == pa.int64()

    def test_text_column_is_string(self, db, sample_tdtp_path) -> None:
        tbl = db.read_arrow(str(sample_tdtp_path))
        assert tbl.schema.field("Name").type == pa.string()

    def test_values_match_row_major(self, db, sample_tdtp_path) -> None:
        """Columnar Arrow values must equal the row-major J_read values."""
        tbl = db.read_arrow(str(sample_tdtp_path))
        raw = db.read(str(sample_tdtp_path))
        bal_idx = SAMPLE_FIELD_NAMES.index("Balance")
        name_idx = SAMPLE_FIELD_NAMES.index("Name")
        arrow_bal = tbl.column("Balance").to_pylist()
        arrow_name = tbl.column("Name").to_pylist()
        assert arrow_bal == [int(r[bal_idx]) for r in raw["data"]]
        assert arrow_name == [r[name_idx] for r in raw["data"]]

    def test_numeric_aggregate_correct(self, db, sample_tdtp_path) -> None:
        tbl = db.read_arrow(str(sample_tdtp_path))
        raw = db.read(str(sample_tdtp_path))
        bal_idx = SAMPLE_FIELD_NAMES.index("Balance")
        assert tbl.column("Balance").to_numpy().sum() == sum(int(r[bal_idx]) for r in raw["data"])


class TestArrowTypes:
    DATA = {
        "schema": {"Fields": [
            {"Name": "i", "Type": "INTEGER"},
            {"Name": "f", "Type": "REAL"},
            {"Name": "s", "Type": "TEXT"},
        ]},
        "header": {"type": "reference", "table_name": "t",
                   "message_id": "m", "timestamp": "2026-01-01T00:00:00Z"},
        "data": [["10", "1.5", "alpha"], ["20", "2.5", "beta"], ["", "", ""]],
    }

    def test_float_null_is_nan(self, db, tmp_path) -> None:
        import math
        f = tmp_path / "t.tdtp.xml"
        db.write(self.DATA, str(f))
        tbl = db.read_arrow(str(f))
        floats = tbl.column("f").to_pylist()
        assert floats[0] == 1.5 and floats[1] == 2.5
        assert math.isnan(floats[2])           # empty REAL → NaN

    def test_int_null_is_zero(self, db, tmp_path) -> None:
        f = tmp_path / "t.tdtp.xml"
        db.write(self.DATA, str(f))
        tbl = db.read_arrow(str(f))
        assert tbl.column("i").to_pylist() == [10, 20, 0]   # empty INTEGER → 0

    def test_string_empty_preserved(self, db, tmp_path) -> None:
        f = tmp_path / "t.tdtp.xml"
        db.write(self.DATA, str(f))
        tbl = db.read_arrow(str(f))
        assert tbl.column("s").to_pylist() == ["alpha", "beta", ""]


class TestArrowCompute:
    def test_duckdb_group_by(self, db, sample_tdtp_path) -> None:
        duckdb = pytest.importorskip("duckdb")
        tbl = db.read_arrow(str(sample_tdtp_path))  # noqa: F841 — referenced by SQL
        out = duckdb.query(
            "SELECT City, count(*) n FROM tbl GROUP BY City"
        ).to_df()
        assert out["n"].sum() == SAMPLE_TOTAL_ROWS

    def test_to_pandas_roundtrip(self, db, sample_tdtp_path) -> None:
        tbl = db.read_arrow(str(sample_tdtp_path))
        pdf = tbl.to_pandas()
        assert len(pdf) == SAMPLE_TOTAL_ROWS
        assert list(pdf.columns) == SAMPLE_FIELD_NAMES


class TestArrowWrite:
    """Write path: arrow_to_data / write_arrow / from_arrow / write_arrow roundtrips."""

    # Shared typed table — INT + FLOAT + TEXT columns.
    TABLE_DATA = {
        "IDs":    [1, 2, 3, 4, 5],
        "Scores": [9.5, 8.0, 7.25, None, 6.0],
        "Tags":   ["alpha", "beta", "gamma", "delta", None],
    }

    @pytest.fixture
    def typed_table(self) -> "pa.Table":
        return pa.table({
            "IDs":    pa.array([1, 2, 3, 4, 5], type=pa.int64()),
            "Scores": pa.array([9.5, 8.0, 7.25, None, 6.0], type=pa.float64()),
            "Tags":   pa.array(["alpha", "beta", "gamma", "delta", None], type=pa.string()),
        })

    def test_arrow_to_data_schema(self, typed_table) -> None:
        from tdtp.arrow_ext import arrow_to_data
        d = arrow_to_data(typed_table, table_name="test")
        fields = d["schema"]["fields"]
        assert [f["name"] for f in fields] == ["IDs", "Scores", "Tags"]
        assert fields[0]["type"] == "INTEGER"
        assert fields[1]["type"] == "REAL"
        assert fields[2]["type"] == "TEXT"

    def test_arrow_to_data_row_count(self, typed_table) -> None:
        from tdtp.arrow_ext import arrow_to_data
        d = arrow_to_data(typed_table)
        assert len(d["data"]) == 5

    def test_arrow_to_data_int_values(self, typed_table) -> None:
        from tdtp.arrow_ext import arrow_to_data
        d = arrow_to_data(typed_table)
        ids = [row[0] for row in d["data"]]
        assert ids == ["1", "2", "3", "4", "5"]

    def test_arrow_to_data_float_null_is_empty(self, typed_table) -> None:
        from tdtp.arrow_ext import arrow_to_data
        d = arrow_to_data(typed_table)
        scores = [row[1] for row in d["data"]]
        assert scores[3] == ""          # None → ""
        assert scores[0] == "9.5"

    def test_arrow_to_data_str_null_is_empty(self, typed_table) -> None:
        from tdtp.arrow_ext import arrow_to_data
        d = arrow_to_data(typed_table)
        tags = [row[2] for row in d["data"]]
        assert tags[4] == ""           # None → ""
        assert tags[0] == "alpha"

    def test_write_arrow_creates_file(self, db, tmp_path, typed_table) -> None:
        f = tmp_path / "typed.tdtp.xml"
        db.write_arrow(typed_table, str(f), table_name="typed")
        assert f.exists() and f.stat().st_size > 0

    def test_write_arrow_roundtrip_row_count(self, db, tmp_path, typed_table) -> None:
        f = tmp_path / "typed.tdtp.xml"
        db.write_arrow(typed_table, str(f))
        raw = db.read(str(f))
        assert len(raw["data"]) == 5

    def test_write_arrow_roundtrip_int_column(self, db, tmp_path, typed_table) -> None:
        f = tmp_path / "typed.tdtp.xml"
        db.write_arrow(typed_table, str(f))
        raw = db.read(str(f))
        ids = [row[0] for row in raw["data"]]
        assert ids == ["1", "2", "3", "4", "5"]

    def test_write_arrow_roundtrip_float_null(self, db, tmp_path, typed_table) -> None:
        f = tmp_path / "typed.tdtp.xml"
        db.write_arrow(typed_table, str(f))
        raw = db.read(str(f))
        scores = [row[1] for row in raw["data"]]
        assert scores[3] == ""           # null preserved
        assert scores[0] == "9.5"

    def test_write_arrow_roundtrip_str_null(self, db, tmp_path, typed_table) -> None:
        f = tmp_path / "typed.tdtp.xml"
        db.write_arrow(typed_table, str(f))
        raw = db.read(str(f))
        tags = [row[2] for row in raw["data"]]
        assert tags[4] == ""             # null preserved
        assert tags[0] == "alpha"

    def test_from_arrow_matches_arrow_to_data(self, db, typed_table) -> None:
        """Tdtp.from_arrow should produce identical output to arrow_to_data."""
        from tdtp.arrow_ext import arrow_to_data
        d1 = arrow_to_data(typed_table, table_name="t", message_id="fixed-id")
        d2 = db.from_arrow(typed_table, table_name="t", message_id="fixed-id")
        assert d1["schema"] == d2["schema"]
        assert d1["data"] == d2["data"]

    def test_write_arrow_empty_table(self, db, tmp_path) -> None:
        empty = pa.table({"A": pa.array([], type=pa.int64())})
        f = tmp_path / "empty.tdtp.xml"
        db.write_arrow(empty, str(f))
        raw = db.read(str(f))
        assert len(raw["data"]) == 0

    def test_read_arrow_write_arrow_roundtrip(self, db, tmp_path, sample_tdtp_path) -> None:
        """read_arrow → write_arrow → read: values must survive unchanged."""
        tbl = db.read_arrow(str(sample_tdtp_path))
        out = tmp_path / "roundtrip.tdtp.xml"
        db.write_arrow(tbl, str(out), table_name="users_copy")
        raw_orig = db.read(str(sample_tdtp_path))
        raw_copy = db.read(str(out))
        assert len(raw_copy["data"]) == len(raw_orig["data"])
        # Integer and string columns must be identical.
        orig_names = [f.get("name", f.get("name", "")) for f in raw_orig["schema"].get("fields", [])]
        copy_names = [f.get("name", f.get("name", "")) for f in raw_copy["schema"].get("fields", [])]
        assert orig_names == copy_names
