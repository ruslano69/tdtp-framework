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
