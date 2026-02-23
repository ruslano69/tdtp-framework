"""
Tests for pandas integration (pandas_ext.py + J_to_pandas / J_from_pandas / PacketHandle.to_pandas).

All tests are skipped automatically if pandas is not installed.
"""
from __future__ import annotations

import pytest

pd = pytest.importorskip("pandas")

from tdtp import TDTPClientJSON, TDTPClientDirect
from tdtp.pandas_ext import data_to_pandas, pandas_to_data

from conftest import (
    SAMPLE_FIELD_NAMES,
    SAMPLE_TOTAL_ROWS,
    SAMPLE_BALANCE_GT_1000_COUNT,
)


# ---------------------------------------------------------------------------
# data_to_pandas — standalone function
# ---------------------------------------------------------------------------

class TestDataToPandas:
    def test_returns_dataframe(self, sample_data_j) -> None:
        df = data_to_pandas(sample_data_j)
        assert isinstance(df, pd.DataFrame)

    def test_row_count(self, sample_data_j) -> None:
        df = data_to_pandas(sample_data_j)
        assert len(df) == SAMPLE_TOTAL_ROWS

    def test_column_names(self, sample_data_j) -> None:
        df = data_to_pandas(sample_data_j)
        assert list(df.columns) == SAMPLE_FIELD_NAMES

    def test_integer_column_typed(self, sample_data_j) -> None:
        df = data_to_pandas(sample_data_j)
        # ID field is INTEGER → should be Int64 (nullable int)
        assert str(df["ID"].dtype) in ("Int64", "int64")

    def test_decimal_column_typed(self, sample_data_j) -> None:
        df = data_to_pandas(sample_data_j)
        # Balance is stored as INTEGER in users.tdtp.xml → Int64;
        # for DECIMAL/REAL fields the dtype would be float64.
        assert str(df["Balance"].dtype) in ("Int64", "float64")

    def test_boolean_column_typed(self, sample_data_j) -> None:
        df = data_to_pandas(sample_data_j)
        # IsActive is stored as INTEGER in users.tdtp.xml → Int64;
        # for BOOLEAN fields the dtype would be boolean.
        assert str(df["IsActive"].dtype) in ("boolean", "bool", "Int64")

    def test_text_column_type(self, sample_data_j) -> None:
        df = data_to_pandas(sample_data_j)
        # pandas 2.x returns StringDtype for TEXT columns; older returns object
        assert df["Name"].dtype in (object, "object") or str(df["Name"].dtype) in ("string", "str")

    def test_pandas_filter_on_typed_column(self, sample_data_j) -> None:
        df = data_to_pandas(sample_data_j)
        filtered = df[df["Balance"] > 1000]
        assert len(filtered) == SAMPLE_BALANCE_GT_1000_COUNT

    def test_empty_string_becomes_none_for_text(self, nullable_data_j) -> None:
        df = data_to_pandas(nullable_data_j)
        # City column has nulls (empty strings in TDTP) → should be None in pandas
        null_cities = df["City"].isna().sum()
        assert null_cities > 0


# ---------------------------------------------------------------------------
# pandas_to_data — standalone function
# ---------------------------------------------------------------------------

class TestPandasToData:
    def test_returns_dict_with_required_keys(self) -> None:
        df = pd.DataFrame({"A": [1, 2], "B": ["x", "y"]})
        result = pandas_to_data(df, table_name="test")
        assert "schema" in result
        assert "header" in result
        assert "data"   in result

    def test_table_name_in_header(self) -> None:
        df = pd.DataFrame({"x": [1]})
        result = pandas_to_data(df, table_name="my_table")
        assert result["header"]["table_name"] == "my_table"

    def test_column_names_preserved(self) -> None:
        df = pd.DataFrame({"Alpha": [1], "Beta": ["hello"]})
        result = pandas_to_data(df)
        field_names = [f["Name"] for f in result["schema"]["Fields"]]
        assert field_names == ["Alpha", "Beta"]

    def test_row_count_preserved(self) -> None:
        df = pd.DataFrame({"n": range(10)})
        result = pandas_to_data(df)
        assert len(result["data"]) == 10

    def test_all_values_are_strings(self) -> None:
        df = pd.DataFrame({"n": [1, 2, 3], "f": [1.1, 2.2, 3.3]})
        result = pandas_to_data(df)
        for row in result["data"]:
            for v in row:
                assert isinstance(v, str)

    def test_na_becomes_empty_string(self) -> None:
        df = pd.DataFrame({"city": ["Moscow", None, "Omsk"]})
        result = pandas_to_data(df)
        assert result["data"][1][0] == ""

    def test_int_type_maps_to_integer(self) -> None:
        df = pd.DataFrame({"id": pd.array([1, 2, 3], dtype="Int64")})
        result = pandas_to_data(df)
        assert result["schema"]["Fields"][0]["Type"] == "INTEGER"

    def test_float_type_maps_to_real(self) -> None:
        df = pd.DataFrame({"price": [1.5, 2.5]})
        result = pandas_to_data(df)
        assert result["schema"]["Fields"][0]["Type"] in ("REAL", "TEXT")

    def test_bool_type_maps_to_boolean(self) -> None:
        df = pd.DataFrame({"active": pd.array([True, False], dtype="boolean")})
        result = pandas_to_data(df)
        assert result["schema"]["Fields"][0]["Type"] == "BOOLEAN"


# ---------------------------------------------------------------------------
# Round-trip через файловую систему: pandas → J_write → файл → J_read → pandas
#
# Эти тесты намеренно гоняют данные через нативный Go-парсер (J_write/J_read),
# а не только через Python-словари. Именно так ловятся несовместимости формата:
# неправильный регистр типа, пустой MessageID, "True" вместо "true",
# нестандартный формат timestamp из внешней БД (MSSQL, Oracle и т.д.)
# ---------------------------------------------------------------------------

class TestRoundTrip:
    def test_roundtrip_data_preserved(self, j_client: TDTPClientJSON, tmp_path) -> None:
        df_orig = pd.DataFrame({
            "ID":      pd.array([1, 2, 3], dtype="Int64"),
            "Name":    ["Alice", "Bob", "Charlie"],
            "Balance": [100.5, 200.0, 0.0],
            "Active":  pd.array([True, False, True], dtype="boolean"),
        })

        # pandas → TDTP dict → write XML
        data = pandas_to_data(df_orig, table_name="roundtrip")
        out  = tmp_path / "roundtrip.tdtp.xml"
        j_client.J_write(data, str(out))

        # read XML → pandas
        raw_back = j_client.J_read(str(out))
        df_back  = data_to_pandas(raw_back)

        assert list(df_back.columns) == list(df_orig.columns)
        assert len(df_back) == len(df_orig)
        # Names must survive the round-trip
        assert list(df_back["Name"]) == list(df_orig["Name"])

    def test_roundtrip_via_client_methods(self, j_client: TDTPClientJSON, tmp_path) -> None:
        df_orig = pd.DataFrame({"X": [10, 20], "Y": ["a", "b"]})

        data = j_client.J_from_pandas(df_orig, table_name="via_client")
        out  = tmp_path / "via_client.tdtp.xml"
        j_client.J_write(data, str(out))

        raw_back = j_client.J_read(str(out))
        df_back  = j_client.J_to_pandas(raw_back)

        assert list(df_back["Y"]) == ["a", "b"]

    def test_roundtrip_boolean_values(self, j_client: TDTPClientJSON, tmp_path) -> None:
        """bool → "true"/"false" → J_write → J_read → обратно."""
        df_orig = pd.DataFrame({
            "ID":     pd.array([1, 2], dtype="Int64"),
            "Active": pd.array([True, False], dtype="boolean"),
        })
        data = pandas_to_data(df_orig, table_name="bool_test")
        out  = tmp_path / "bool.tdtp.xml"
        j_client.J_write(data, str(out))
        raw  = j_client.J_read(str(out))

        # Файл должен читаться без ошибок — это основная проверка
        assert len(raw["data"]) == 2
        # Значения в TDTP хранятся как строки; "true"/"false", не "True"/"False"
        active_idx = [f["Name"] for f in raw["schema"]["Fields"]].index("Active")
        assert raw["data"][0][active_idx] == "true"
        assert raw["data"][1][active_idx] == "false"

    def test_roundtrip_nullable_integers(self, j_client: TDTPClientJSON, tmp_path) -> None:
        """NULL в Int64 → пустая строка → J_write → J_read → pd.NA."""
        df_orig = pd.DataFrame({
            "ID":      pd.array([1, 2, 3], dtype="Int64"),
            "Balance": pd.array([500, pd.NA, 1500], dtype="Int64"),
        })
        data = pandas_to_data(df_orig, table_name="nullable_test")
        out  = tmp_path / "nullable.tdtp.xml"
        j_client.J_write(data, str(out))
        raw  = j_client.J_read(str(out))
        df   = data_to_pandas(raw)

        bal_idx = [f["Name"] for f in raw["schema"]["Fields"]].index("Balance")
        # NULL в TDTP → пустая строка в raw["data"]
        assert raw["data"][1][bal_idx] == ""
        # После data_to_pandas → pd.NA
        assert pd.isna(df["Balance"].iloc[1])

    def test_roundtrip_timestamp_iso8601(self, j_client: TDTPClientJSON, tmp_path) -> None:
        """ISO 8601 timestamp проходит round-trip как TEXT без потерь."""
        ts_values = [
            "2025-11-10T15:30:00Z",      # UTC с Z
            "2025-11-12T09:15:00+03:00", # со смещением (как из MSSQL/datetimeoffset)
            "2025-03-10 12:00:00",        # без T (SQLite/MSSQL datetime без TZ)
        ]
        df_orig = pd.DataFrame({"ID": pd.array([1, 2, 3], dtype="Int64"),
                                 "CreatedAt": ts_values})
        data = pandas_to_data(df_orig, table_name="ts_test")
        out  = tmp_path / "ts.tdtp.xml"
        j_client.J_write(data, str(out))
        raw  = j_client.J_read(str(out))
        df   = data_to_pandas(raw)

        # Строки должны дойти без искажений — формат не интерпретируется TDTP
        assert list(df["CreatedAt"]) == ts_values

    def test_roundtrip_message_id_autogenerated(self, j_client: TDTPClientJSON, tmp_path) -> None:
        """pandas_to_data() без явного message_id должен генерировать UUID,
        иначе J_read вернёт ошибку 'header.MessageID is required'."""
        import re
        df   = pd.DataFrame({"x": [1]})
        data = pandas_to_data(df)
        # UUID4 формат
        assert re.match(
            r"[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}",
            data["header"]["message_id"],
        )
        # Файл должен читаться — проверка что Go-парсер принял MessageID
        out = tmp_path / "uuid_check.tdtp.xml"
        j_client.J_write(data, str(out))
        raw = j_client.J_read(str(out))
        assert len(raw["data"]) == 1


# ---------------------------------------------------------------------------
# J_to_pandas / J_from_pandas — client methods
# ---------------------------------------------------------------------------

class TestClientMethods:
    def test_j_to_pandas_returns_dataframe(self, j_client, sample_data_j) -> None:
        df = j_client.J_to_pandas(sample_data_j)
        assert isinstance(df, pd.DataFrame)
        assert len(df) == SAMPLE_TOTAL_ROWS

    def test_j_from_pandas_returns_dict(self, j_client) -> None:
        df = pd.DataFrame({"col": [1, 2, 3]})
        result = j_client.J_from_pandas(df, table_name="t")
        assert isinstance(result, dict)
        assert result["header"]["table_name"] == "t"


# ---------------------------------------------------------------------------
# PacketHandle.to_pandas — D_* API
# ---------------------------------------------------------------------------

class TestPacketHandleToPandas:
    def test_to_pandas_returns_dataframe(self, d_client: TDTPClientDirect, sample_tdtp_path) -> None:
        with d_client.D_read_ctx(str(sample_tdtp_path)) as pkt:
            df = pkt.to_pandas()
        assert isinstance(df, pd.DataFrame)
        assert len(df) == SAMPLE_TOTAL_ROWS

    def test_to_pandas_column_names(self, d_client: TDTPClientDirect, sample_tdtp_path) -> None:
        with d_client.D_read_ctx(str(sample_tdtp_path)) as pkt:
            df = pkt.to_pandas()
        assert list(df.columns) == SAMPLE_FIELD_NAMES
