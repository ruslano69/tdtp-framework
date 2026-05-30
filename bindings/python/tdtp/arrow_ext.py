"""
Apache Arrow bridge — columnar read and write for TDTP packets.

Read path: builds a ``pyarrow.Table`` from a Direct packet by extracting each
column as a contiguous typed buffer in Go (one pass per column), instead of
materializing every row as Python objects.

Write path: converts a ``pyarrow.Table`` to a TDTP file via vectorized numpy
column extraction and the ``J_WriteColumnar`` Go function, which transposes
column-major arrays to row-major internally — far faster than the row-by-row
``itertuples`` + ``_serialize`` path for large numeric datasets.

Optional — requires pyarrow + numpy:  pip install tdtp[arrow]

Public API
----------
packet_to_arrow(handle)              PacketHandle  → pyarrow.Table
arrow_to_data(table, ...)            pyarrow.Table → TDTP data dict
write_arrow(table, path, ...)        pyarrow.Table → writes TDTP file

Null handling:
    Read:
        FLOAT/REAL columns  → NaN marks null.
        INTEGER columns     → 0 marks null (int64 has no NaN).
    Write:
        Arrow null / NaN    → empty string (TDTP null convention).
        Infinite float      → empty string.
"""
from __future__ import annotations

import ctypes as _ctypes
import json as _json
import uuid as _uuid
from datetime import datetime, timezone
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    import pyarrow as pa

    from tdtp.api_d import PacketHandle

try:
    import numpy as _np
    import pyarrow as _pa
    HAS_ARROW = True
except ImportError:
    HAS_ARROW = False


# TDTP canonical type → Arrow extraction strategy ("int" | "float" | "str")
_INT_TYPES   = {"INTEGER", "INT", "BIGINT", "SMALLINT", "TINYINT"}
_FLOAT_TYPES = {"REAL", "FLOAT", "DOUBLE", "DECIMAL", "NUMERIC"}

# Arrow type → TDTP canonical type (write direction)
_ARROW_INT_TO_TDTP   = "INTEGER"
_ARROW_FLOAT_TO_TDTP = "REAL"
_ARROW_STR_TO_TDTP   = "TEXT"


def _require_arrow() -> None:
    if not HAS_ARROW:
        raise ImportError(
            "pyarrow and numpy are not installed. "
            "Install them with:  pip install tdtp[arrow]"
        )


# ---------------------------------------------------------------------------
# Read path helpers (D_Packet → Arrow column arrays)
# ---------------------------------------------------------------------------

def _float_column(lib: Any, pkt_ref: Any, col: int, n: int) -> "pa.Array":
    ptr = lib.D_ColumnFloat64(pkt_ref, col)
    try:
        view = _np.ctypeslib.as_array(ptr, shape=(n,))
        arr = view.copy()
    finally:
        lib.D_FreeBuffer(_ctypes.cast(ptr, _ctypes.c_void_p))
    return _pa.array(arr)  # NaN propagates through Arrow arithmetic


def _int_column(lib: Any, pkt_ref: Any, col: int, n: int) -> "pa.Array":
    ptr = lib.D_ColumnInt64(pkt_ref, col)
    try:
        view = _np.ctypeslib.as_array(ptr, shape=(n,))
        arr = view.copy()
    finally:
        lib.D_FreeBuffer(_ctypes.cast(ptr, _ctypes.c_void_p))
    return _pa.array(arr)


def _utf8_column(lib: Any, pkt_ref: Any, col: int, n: int) -> "pa.Array":
    off_pp = _ctypes.POINTER(_ctypes.c_int)()
    nbytes = _ctypes.c_int()
    data_ptr = lib.D_ColumnUTF8(
        pkt_ref, col, _ctypes.byref(off_pp), _ctypes.byref(nbytes)
    )
    try:
        data_bytes = _ctypes.string_at(data_ptr, nbytes.value)
        off_bytes = _ctypes.string_at(
            _ctypes.cast(off_pp, _ctypes.c_void_p), (n + 1) * 4
        )
        data_buf = _pa.py_buffer(data_bytes)
        off_buf  = _pa.py_buffer(off_bytes)
        return _pa.Array.from_buffers(_pa.string(), n, [None, off_buf, data_buf])
    finally:
        lib.D_FreeBuffer(_ctypes.cast(data_ptr, _ctypes.c_void_p))
        lib.D_FreeBuffer(_ctypes.cast(off_pp, _ctypes.c_void_p))


# ---------------------------------------------------------------------------
# Write path helpers (Arrow column → list[str])
# ---------------------------------------------------------------------------

def _arrow_col_to_strings(col: "pa.Array") -> list:
    """Convert one Arrow column to a list of TDTP string values.

    Nulls, NaN and ±Inf all become the empty string (TDTP null convention).
    Uses numpy for numeric columns to avoid per-element Python dispatch.
    """
    arr_type = col.type

    if _pa.types.is_integer(arr_type):
        np_arr = col.to_numpy(zero_copy_only=False)  # fills nulls with 0
        result = np_arr.astype(str).tolist()
        if col.null_count:
            mask = col.is_null().to_pylist()
            for i, is_null in enumerate(mask):
                if is_null:
                    result[i] = ""
        return result

    if _pa.types.is_floating(arr_type):
        np_arr = col.to_numpy(zero_copy_only=False)   # NaN for nulls
        # Nulls from Arrow validity bitmap may not be NaN; OR them in.
        if col.null_count:
            null_mask = col.is_null().to_numpy()
            np_arr = _np.where(null_mask, _np.nan, np_arr)
        # Replace NaN and ±Inf with empty string.
        bad = ~_np.isfinite(np_arr)
        str_arr = _np.where(bad, "", np_arr.astype(str))
        return str_arr.tolist()

    # String / binary / other: fall back to pylist (no numpy speedup available)
    vals = col.to_pylist()
    return ["" if v is None else str(v) for v in vals]


def _arrow_type_to_tdtp(arrow_type: "pa.DataType") -> str:
    """Map an Arrow type to the closest TDTP canonical type."""
    if _pa.types.is_integer(arrow_type):
        return _ARROW_INT_TO_TDTP
    if _pa.types.is_floating(arrow_type):
        return _ARROW_FLOAT_TO_TDTP
    if _pa.types.is_boolean(arrow_type):
        return "BOOLEAN"
    if _pa.types.is_timestamp(arrow_type) or _pa.types.is_date(arrow_type):
        return "DATETIME"
    return _ARROW_STR_TO_TDTP


# ---------------------------------------------------------------------------
# Public API — read direction
# ---------------------------------------------------------------------------

def packet_to_arrow(handle: "PacketHandle") -> "pa.Table":
    """Convert a Direct PacketHandle to a ``pyarrow.Table`` column by column.

    Each column is extracted as a typed C buffer in Go (one pass) rather than
    row-by-row in Python — far faster on large datasets. The resulting table
    feeds pandas, polars and DuckDB directly.

    Args:
        handle: a :class:`~tdtp.api_d.PacketHandle` from ``TDTPClientDirect.D_read_ctx``.

    Returns:
        ``pyarrow.Table``.  Numeric fields → int64/float64; everything else → string.

    Raises:
        ImportError: if pyarrow or numpy is not installed.
    """
    _require_arrow()
    from tdtp._loader import lib

    pkt     = handle.pkt
    pkt_ref = _ctypes.byref(pkt)
    n       = int(pkt.row_count)
    schema  = handle.get_schema()

    columns: list["pa.Array"] = []
    names:   list[str]        = []

    for col, field in enumerate(schema):
        name      = field.get("name", f"col{col}")
        tdtp_type = str(field.get("type", "TEXT")).upper()
        names.append(name)
        if n == 0:
            columns.append(_pa.array([], type=_pa.string()))
        elif tdtp_type in _INT_TYPES:
            columns.append(_int_column(lib, pkt_ref, col, n))
        elif tdtp_type in _FLOAT_TYPES:
            columns.append(_float_column(lib, pkt_ref, col, n))
        else:
            columns.append(_utf8_column(lib, pkt_ref, col, n))

    return _pa.table(columns, names=names)


# ---------------------------------------------------------------------------
# Public API — write direction
# ---------------------------------------------------------------------------

def arrow_to_data(
    table:      "pa.Table",
    table_name: str = "data",
    message_id: str = "",
) -> dict:
    """Convert a ``pyarrow.Table`` to a TDTP data dict.

    Uses numpy vectorized column extraction for numeric types — much faster
    than the row-by-row ``itertuples`` + ``_serialize`` path for large datasets.
    The returned dict has the same structure as ``J_read()`` output and can be
    passed to ``Tdtp.write()`` or ``J_WriteFile``.

    Args:
        table:      Input Arrow table.
        table_name: TDTP ``table_name`` written into the packet header.
        message_id: TDTP ``MessageID``; auto-generated UUID4 when empty.

    Returns:
        dict with ``"schema"``, ``"header"``, and ``"data"`` keys.

    Raises:
        ImportError: if pyarrow or numpy is not installed.
    """
    _require_arrow()

    if not message_id:
        message_id = str(_uuid.uuid4())

    fields: list[dict] = []
    col_strs: list[list] = []

    for i in range(table.num_columns):
        col  = table.column(i)
        name = table.schema.names[i]
        tdtp_type = _arrow_type_to_tdtp(col.type)
        fields.append({"Name": name, "Type": tdtp_type})
        col_strs.append(_arrow_col_to_strings(col))

    # Transpose column-major → row-major.
    # zip(*col_strs) is a C-level iterator; list() materializes in one pass.
    n    = len(table)
    rows = [list(row) for row in zip(*col_strs)] if col_strs and n else []

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


def write_arrow(
    table:      "pa.Table",
    path:       str,
    table_name: str = "data",
    message_id: str = "",
) -> None:
    """Write a ``pyarrow.Table`` to a TDTP file using the columnar write path.

    Extracts each column as a numpy array, serialises to strings vectorized,
    then passes column-major data to the Go ``J_WriteColumnar`` function which
    transposes and writes in one allocation — faster than building a row-major
    Python dict and calling ``J_WriteFile``.

    Args:
        table:      Arrow table to write.
        path:       Output file path (``*.tdtp.xml`` convention).
        table_name: Packet ``table_name`` header value.
        message_id: Packet ``MessageID``; auto-generated UUID4 when empty.

    Raises:
        ImportError:  if pyarrow or numpy is not installed.
        RuntimeError: if the Go write fails.
    """
    _require_arrow()
    from tdtp._loader import lib, free_string

    if not message_id:
        message_id = str(_uuid.uuid4())

    fields: list[dict] = []
    columns: list[list] = []

    for i in range(table.num_columns):
        col  = table.column(i)
        name = table.schema.names[i]
        tdtp_type = _arrow_type_to_tdtp(col.type)
        fields.append({"Name": name, "Type": tdtp_type})
        columns.append(_arrow_col_to_strings(col))

    payload = {
        "schema":  {"Fields": fields},
        "header": {
            "type":       "reference",
            "table_name": table_name,
            "message_id": message_id,
            "timestamp":  datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        },
        "columns": columns,
    }

    json_bytes = _json.dumps(payload, ensure_ascii=False).encode("utf-8")
    path_bytes = path.encode("utf-8")

    result_ptr = lib.J_WriteColumnar(
        _ctypes.c_char_p(json_bytes),
        _ctypes.c_char_p(path_bytes),
    )
    if not result_ptr:
        raise RuntimeError("J_WriteColumnar returned NULL")
    raw = _ctypes.string_at(result_ptr)
    free_string(result_ptr)

    resp = _json.loads(raw)
    if "error" in resp:
        raise RuntimeError(f"J_WriteColumnar error: {resp['error']}")
