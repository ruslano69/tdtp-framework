"""
Tests for pandas integration (pandas_ext.py + J_to_pandas / J_from_pandas / PacketHandle.to_pandas).

All tests are skipped automatically if pandas is not installed.
"""
from __future__ import annotations

import base64
import json
from datetime import datetime, timezone, timedelta

import pytest

pd = pytest.importorskip("pandas")

from tdtp import TDTPClientJSON, TDTPClientDirect
from tdtp.pandas_ext import data_to_pandas, pandas_to_data, _serialize

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


# ---------------------------------------------------------------------------
# _serialize — unit tests for BLOB / TIMESTAMP / JSON correctness
# ---------------------------------------------------------------------------

class TestSerialize:
    """Unit-tests for the _serialize() helper.

    These tests verify that binary, datetime, and JSON values are converted to
    the correct TDTP wire format — not Python's str() representation.
    """

    # --- BLOB / bytes ---

    def test_bytes_to_base64(self) -> None:
        """bytes → Base64 string (TDTP BLOB standard, matches Go base64.StdEncoding)."""
        raw = b"\x00\xff\xde\xad\xbe\xef"
        result = _serialize(raw)
        assert result == base64.b64encode(raw).decode("ascii")
        # Verify it is valid Base64 (no Python-repr artefacts like b'...')
        assert base64.b64decode(result) == raw

    def test_bytearray_to_base64(self) -> None:
        raw = bytearray(b"\x01\x02\x03")
        assert _serialize(raw) == base64.b64encode(raw).decode("ascii")

    def test_empty_bytes_to_empty_base64(self) -> None:
        assert _serialize(b"") == ""   # base64 of empty bytes is ""

    # --- TIMESTAMP / datetime ---

    def test_naive_datetime_utc_z(self) -> None:
        """Naive datetime → UTC suffix Z (TDTP convention)."""
        dt = datetime(2025, 11, 10, 15, 30, 0)
        assert _serialize(dt) == "2025-11-10T15:30:00Z"

    def test_aware_datetime_utc(self) -> None:
        """UTC-aware datetime → RFC3339 with +00:00 offset."""
        dt = datetime(2025, 11, 10, 15, 30, 0, tzinfo=timezone.utc)
        result = _serialize(dt)
        # isoformat() for UTC produces "+00:00"; both "+00:00" and "Z" are RFC3339
        assert result in ("2025-11-10T15:30:00+00:00", "2025-11-10T15:30:00Z")

    def test_aware_datetime_positive_offset(self) -> None:
        """tz-aware datetime with +03:00 offset → normalised to UTC RFC3339.
        Go calls .UTC().Format(time.RFC3339) — matches all Go adapters."""
        tz3 = timezone(timedelta(hours=3))
        dt  = datetime(2025, 11, 12, 9, 15, 0, tzinfo=tz3)
        result = _serialize(dt)
        assert result == "2025-11-12T06:15:00Z"

    def test_pandas_timestamp_naive(self) -> None:
        """pd.Timestamp (naive) → same RFC3339 as plain datetime."""
        ts = pd.Timestamp("2025-03-10 12:00:00")
        assert _serialize(ts) == "2025-03-10T12:00:00Z"

    def test_pandas_timestamp_utc(self) -> None:
        ts = pd.Timestamp("2025-11-10T15:30:00", tz="UTC")
        result = _serialize(ts)
        assert "2025-11-10T15:30:00" in result

    def test_microseconds_stripped(self) -> None:
        """Sub-second precision is stripped to keep RFC3339 compact (matches Go)."""
        dt = datetime(2025, 1, 1, 0, 0, 0, 123456, tzinfo=timezone.utc)
        result = _serialize(dt)
        assert "123456" not in result
        assert "." not in result

    # --- JSON / JSONB ---

    def test_dict_to_json_double_quotes(self) -> None:
        """dict → JSON string with double quotes (not Python repr with single quotes)."""
        d = {"key": "value", "n": 42}
        result = _serialize(d)
        parsed = json.loads(result)   # must be valid JSON
        assert parsed == d
        assert "'" not in result      # no Python-style single quotes

    def test_dict_bool_lowercase(self) -> None:
        """Python True/False in dict → JSON 'true'/'false' (not 'True'/'False')."""
        d = {"active": True, "deleted": False}
        result = _serialize(d)
        assert "true" in result
        assert "false" in result
        assert "True" not in result
        assert "False" not in result

    def test_list_to_json(self) -> None:
        lst = [1, "two", True, None]
        result = _serialize(lst)
        parsed = json.loads(result)
        assert parsed == lst

    def test_nested_json(self) -> None:
        d = {"meta": {"tags": ["a", "b"]}, "count": 3}
        result = _serialize(d)
        assert json.loads(result) == d

    def test_unicode_preserved_in_json(self) -> None:
        """Unicode characters must not be escaped as \\uXXXX."""
        d = {"name": "Привет"}
        result = _serialize(d)
        assert "Привет" in result
        assert "\\u" not in result


# ---------------------------------------------------------------------------
# Round-trip: pandas → J_write → J_read → pandas для новых типов
# ---------------------------------------------------------------------------

class TestRoundTripNewTypes:
    def test_roundtrip_blob_column(self, j_client: TDTPClientJSON, tmp_path) -> None:
        """bytes column survives round-trip as Base64 TEXT."""
        payloads = [b"\x00\x01\x02", b"\xde\xad\xbe\xef", b"hello"]
        df_orig = pd.DataFrame({
            "ID":      pd.array([1, 2, 3], dtype="Int64"),
            "Payload": payloads,
        })
        data = pandas_to_data(df_orig, table_name="blob_test")
        out  = tmp_path / "blob.tdtp.xml"
        j_client.J_write(data, str(out))
        raw  = j_client.J_read(str(out))

        payload_idx = [f["Name"] for f in raw["schema"]["Fields"]].index("Payload")
        for i, raw_bytes in enumerate(payloads):
            expected_b64 = base64.b64encode(raw_bytes).decode("ascii")
            assert raw["data"][i][payload_idx] == expected_b64

    def test_roundtrip_datetime_naive(self, j_client: TDTPClientJSON, tmp_path) -> None:
        """Naive datetime column → RFC3339 string with Z → survives J_write/J_read."""
        dts = [datetime(2025, 1, 15, 10, 0, 0),
               datetime(2024, 6, 30, 23, 59, 59)]
        df_orig = pd.DataFrame({
            "ID": pd.array([1, 2], dtype="Int64"),
            "Ts": dts,
        })
        data = pandas_to_data(df_orig, table_name="dt_test")
        out  = tmp_path / "dt.tdtp.xml"
        j_client.J_write(data, str(out))
        raw  = j_client.J_read(str(out))

        ts_idx = [f["Name"] for f in raw["schema"]["Fields"]].index("Ts")
        assert raw["data"][0][ts_idx] == "2025-01-15T10:00:00Z"
        assert raw["data"][1][ts_idx] == "2024-06-30T23:59:59Z"

    def test_roundtrip_json_dict_column(self, j_client: TDTPClientJSON, tmp_path) -> None:
        """dict column → compact JSON string → survives J_write/J_read."""
        docs = [{"name": "Alice", "active": True},
                {"name": "Bob",   "tags": [1, 2, 3]}]
        df_orig = pd.DataFrame({
            "ID":   pd.array([1, 2], dtype="Int64"),
            "Meta": docs,
        })
        data = pandas_to_data(df_orig, table_name="json_test")
        out  = tmp_path / "json.tdtp.xml"
        j_client.J_write(data, str(out))
        raw  = j_client.J_read(str(out))

        meta_idx = [f["Name"] for f in raw["schema"]["Fields"]].index("Meta")
        # Values in file must be valid JSON with double quotes
        parsed_0 = json.loads(raw["data"][0][meta_idx])
        assert parsed_0["name"] == "Alice"
        assert parsed_0["active"] is True

        parsed_1 = json.loads(raw["data"][1][meta_idx])
        assert parsed_1["tags"] == [1, 2, 3]
