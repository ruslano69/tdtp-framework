"""
Apache Arrow bridge — columnar access to TDTP packets.

Builds a ``pyarrow.Table`` from a Direct packet by extracting each column as a
contiguous typed buffer in Go (one pass per column), instead of materializing
every row as Python objects. Arrow then feeds pandas, polars and DuckDB.

Optional — requires pyarrow:  pip install tdtp[arrow]

Public API
----------
packet_to_arrow(handle)   PacketHandle → pyarrow.Table

Null handling (v1):
    FLOAT/REAL columns  → NaN marks null.
    INTEGER columns     → 0 marks null (int64 has no NaN). Use the float path
                          if you must distinguish 0 from null.
"""
from __future__ import annotations

import ctypes as _ctypes
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    import pyarrow as pa

    from tdtp.api_d import PacketHandle

try:
    import numpy as _np
    import pyarrow as _pa
    HAS_ARROW = True
except ImportError:
    HAS_ARROW = False


# TDTP canonical type → "int" | "float" | "str" extraction strategy
_INT_TYPES = {"INTEGER", "INT", "BIGINT", "SMALLINT", "TINYINT"}
_FLOAT_TYPES = {"REAL", "FLOAT", "DOUBLE", "DECIMAL", "NUMERIC"}


def _require_arrow() -> None:
    if not HAS_ARROW:
        raise ImportError(
            "pyarrow is not installed. Install it with:  pip install tdtp[arrow]"
        )


def _float_column(lib, pkt_ref, col: int, n: int) -> "pa.Array":
    ptr = lib.D_ColumnFloat64(pkt_ref, col)
    try:
        # Zero-copy view over the C buffer, then own a copy before freeing.
        view = _np.ctypeslib.as_array(ptr, shape=(n,))
        arr = view.copy()
    finally:
        lib.D_FreeBuffer(_ctypes.cast(ptr, _ctypes.c_void_p))
    return _pa.array(arr)  # NaN → null happens at to_pandas/compute time


def _int_column(lib, pkt_ref, col: int, n: int) -> "pa.Array":
    ptr = lib.D_ColumnInt64(pkt_ref, col)
    try:
        view = _np.ctypeslib.as_array(ptr, shape=(n,))
        arr = view.copy()
    finally:
        lib.D_FreeBuffer(_ctypes.cast(ptr, _ctypes.c_void_p))
    return _pa.array(arr)


def _utf8_column(lib, pkt_ref, col: int, n: int) -> "pa.Array":
    off_pp = _ctypes.POINTER(_ctypes.c_int)()
    nbytes = _ctypes.c_int()
    data_ptr = lib.D_ColumnUTF8(pkt_ref, col, _ctypes.byref(off_pp), _ctypes.byref(nbytes))
    try:
        # Copy the two buffers into Arrow-owned memory, then build the string
        # array directly from them (no per-element Python conversion).
        data_bytes = _ctypes.string_at(data_ptr, nbytes.value)
        off_bytes = _ctypes.string_at(
            _ctypes.cast(off_pp, _ctypes.c_void_p), (n + 1) * 4
        )
        data_buf = _pa.py_buffer(data_bytes)
        off_buf = _pa.py_buffer(off_bytes)
        return _pa.Array.from_buffers(_pa.string(), n, [None, off_buf, data_buf])
    finally:
        lib.D_FreeBuffer(_ctypes.cast(data_ptr, _ctypes.c_void_p))
        lib.D_FreeBuffer(_ctypes.cast(off_pp, _ctypes.c_void_p))


def packet_to_arrow(handle: "PacketHandle") -> "pa.Table":
    """Convert a Direct PacketHandle to a ``pyarrow.Table`` column by column.

    Args:
        handle: a :class:`~tdtp.api_d.PacketHandle` from ``TDTPClientDirect.D_read``.

    Returns:
        ``pyarrow.Table`` with one column per schema field; numeric fields land
        as int64/float64, everything else as string.

    Raises:
        ImportError: if pyarrow is not installed.
    """
    _require_arrow()
    from tdtp._loader import lib

    pkt = handle.pkt
    pkt_ref = _ctypes.byref(pkt)
    n = int(pkt.row_count)
    schema = handle.get_schema()

    columns: list["pa.Array"] = []
    names: list[str] = []
    for col, field in enumerate(schema):
        name = field.get("name", f"col{col}")
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
