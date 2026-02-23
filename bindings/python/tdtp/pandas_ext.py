"""
Pandas integration for TDTP Python bindings.

Optional — requires pandas:  pip install pandas

Public API
----------
data_to_pandas(data)          J_read dict  → pandas.DataFrame
pandas_to_data(df, table_name) pandas.DataFrame → J_read dict (ready for J_write)
"""
from __future__ import annotations

import uuid
from datetime import datetime, timezone
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    import pandas as pd

try:
    import pandas as _pd
    HAS_PANDAS = True
except ImportError:
    HAS_PANDAS = False


# ---------------------------------------------------------------------------
# Type mapping tables
# ---------------------------------------------------------------------------

# TDTP canonical type (uppercase) → pandas dtype string
_TDTP_TO_PANDAS: dict[str, str] = {
    "INTEGER":     "Int64",    # nullable int (pd.Int64Dtype)
    "INT":         "Int64",
    "BIGINT":      "Int64",
    "SMALLINT":    "Int64",
    "TINYINT":     "Int64",
    "REAL":        "float64",
    "FLOAT":       "float64",
    "DOUBLE":      "float64",
    "DECIMAL":     "float64",
    "NUMERIC":     "float64",
    "BOOLEAN":     "boolean",  # nullable bool (pd.BooleanDtype)
    "BOOL":        "boolean",
    # Date/time types — kept as object; user can call pd.to_datetime() if needed
    "DATE":        "object",
    "DATETIME":    "object",
    "TIMESTAMP":   "object",
    "TIMESTAMPTZ": "object",
    # Text and misc types
    "TEXT":        "object",
    "VARCHAR":     "object",
    "NVARCHAR":    "object",
    "CHAR":        "object",
    "UUID":        "object",
    "JSONB":       "object",
    "JSON":        "object",
}

# pandas dtype name → TDTP canonical type
_PANDAS_TO_TDTP: dict[str, str] = {
    "int8":           "INTEGER",
    "int16":          "INTEGER",
    "int32":          "INTEGER",
    "int64":          "INTEGER",
    "Int8":           "INTEGER",
    "Int16":          "INTEGER",
    "Int32":          "INTEGER",
    "Int64":          "INTEGER",
    "uint8":          "INTEGER",
    "uint16":         "INTEGER",
    "uint32":         "INTEGER",
    "uint64":         "INTEGER",
    "float16":        "REAL",
    "float32":        "REAL",
    "float64":        "REAL",
    "bool":           "BOOLEAN",
    "boolean":        "BOOLEAN",
    "datetime64[ns]": "DATETIME",
    "object":         "TEXT",
    "string":         "TEXT",
    "category":       "TEXT",
}


# ---------------------------------------------------------------------------
# Internal helpers
# ---------------------------------------------------------------------------

def _require_pandas() -> None:
    if not HAS_PANDAS:
        raise ImportError(
            "pandas is not installed. Install it with:  pip install pandas"
        )


def _tdtp_dtype(tdtp_type: str) -> str:
    """Map a TDTP type string to a pandas dtype string (falls back to 'object')."""
    return _TDTP_TO_PANDAS.get(tdtp_type.upper(), "object")


def _pandas_tdtp_type(dtype) -> str:
    """Map a pandas dtype to a TDTP type string (falls back to 'TEXT')."""
    name = str(dtype)
    # tz-aware datetime, e.g. 'datetime64[ns, UTC]'
    if name.startswith("datetime64"):
        return "DATETIME"
    return _PANDAS_TO_TDTP.get(name, "TEXT")


def _is_na(v) -> bool:
    """Return True if v is any kind of missing value (None, NaN, pd.NA, pd.NaT)."""
    if v is None:
        return True
    try:
        return bool(_pd.isna(v))
    except (TypeError, ValueError):
        return False


def _serialize(v) -> str:
    """Convert a single cell value to a TDTP string representation.

    - None / NaN / pd.NA / pd.NaT  → ""
    - bool / pd.BooleanDtype        → "true" / "false"  (lowercase)
    - float with no fractional part → str(int(v))  e.g. 71160.0 → "71160"
      (matches Go strconv.FormatFloat behavior with -1 precision)
    - everything else               → str(v)
    """
    if _is_na(v):
        return ""
    # bool must be checked before int (bool is subclass of int in Python)
    if isinstance(v, bool):
        return "true" if v else "false"
    try:
        # pd.NA-backed boolean arrays yield numpy.bool_ on iteration
        import numpy as _np
        if isinstance(v, _np.bool_):
            return "true" if v else "false"
        # numpy float with no fractional part: 71160.0 → "71160" (matches Go)
        if isinstance(v, _np.floating) and v.is_integer():
            return str(int(v))
    except ImportError:
        pass
    # Python native float with no fractional part: 71160.0 → "71160"
    if isinstance(v, float) and v.is_integer():
        return str(int(v))
    return str(v)


def _extract_fields(data: dict) -> list[dict]:
    """Extract schema fields from a J_read dict (handles upper/lowercase keys)."""
    schema = data.get("schema", {})
    # J_* API uses uppercase 'Fields'; keep compatible with any casing
    return schema.get("Fields", schema.get("fields", []))


def _field_name(f: dict) -> str:
    return f.get("Name", f.get("name", ""))


def _field_type(f: dict) -> str:
    return f.get("Type", f.get("type", "TEXT"))


# ---------------------------------------------------------------------------
# Core public functions
# ---------------------------------------------------------------------------

def data_to_pandas(data: dict) -> "pd.DataFrame":
    """Convert a J_read result dict to a pandas DataFrame.

    All string values are cast to the dtype indicated by each field's TDTP
    type.  Empty strings (representing SQL NULL) become ``pd.NA`` / ``None``
    for typed columns, and ``None`` for text columns.

    Args:
        data: dict returned by ``TDTPClientJSON.J_read()`` (or ``J_filter()``,
              ``J_apply_processor()``, etc.).  Must have keys ``"schema"``
              and ``"data"``.

    Returns:
        ``pandas.DataFrame`` with columns named after schema fields and dtypes
        inferred from TDTP field types.

    Raises:
        ImportError: if pandas is not installed.

    Example::

        client = TDTPClientJSON()
        raw    = client.J_read("users.tdtp.xml")
        df     = data_to_pandas(raw)
        print(df.dtypes)
        print(df.describe())
    """
    _require_pandas()

    fields  = _extract_fields(data)
    columns = [_field_name(f) for f in fields]
    rows    = data.get("data", [])

    df = _pd.DataFrame(rows, columns=columns)

    for field in fields:
        col   = _field_name(field)
        dtype = _tdtp_dtype(_field_type(field))

        if dtype == "object":
            # Replace empty strings with None for nullable text columns
            df[col] = df[col].replace("", None)
            continue

        try:
            if dtype in ("Int64", "boolean"):
                df[col] = df[col].replace("", _pd.NA)
            df[col] = df[col].astype(dtype)
        except (ValueError, TypeError):
            # Malformed data — keep as object rather than crashing
            pass

    return df


def pandas_to_data(df: "pd.DataFrame", table_name: str = "data", message_id: str = "") -> dict:
    """Convert a pandas DataFrame to a TDTP data dict.

    The returned dict has the same structure as ``J_read()`` output and can be
    passed directly to ``TDTPClientJSON.J_write()``.

    Args:
        df:         ``pandas.DataFrame`` to convert.
        table_name: value written into the TDTP header ``table_name`` field.
        message_id: TDTP ``MessageID`` (required by the parser). If empty,
                    a UUID4 is generated automatically.

    Returns:
        dict with ``"schema"``, ``"header"``, and ``"data"`` keys.

    Raises:
        ImportError: if pandas is not installed.

    Example::

        import pandas as pd
        from tdtp.pandas_ext import pandas_to_data

        df   = pd.read_csv("input.csv")
        data = pandas_to_data(df, table_name="import_table")
        client.J_write(data, "output.tdtp.xml")
    """
    _require_pandas()

    if not message_id:
        message_id = str(uuid.uuid4())

    # Build schema fields (uppercase keys — J_* convention)
    fields = [
        {"Name": str(col), "Type": _pandas_tdtp_type(dtype)}
        for col, dtype in df.dtypes.items()
    ]

    # Serialise rows: every value → TDTP string; booleans → "true"/"false"
    rows: list[list[str]] = []
    for tup in df.itertuples(index=False, name=None):
        rows.append([_serialize(v) for v in tup])

    return {
        "schema": {"Fields": fields},
        "header": {
            "type":       "reference",
            "table_name": table_name,
            "message_id": message_id,
            "timestamp":  datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        },
        "data": rows,
    }
