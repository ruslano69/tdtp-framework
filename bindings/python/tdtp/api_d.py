"""
Direct boundary API — TDTPClientDirect.

Data crosses the Python↔Go boundary as C structs via ctypes pointers.
No JSON serialization overhead; maximum throughput for large datasets.

IMPORTANT — memory management:
    Every D_* method that returns or fills a D_Packet allocates memory
    with C.malloc on the Go side. You MUST call pkt.free() (or the
    context manager) when done; otherwise memory leaks.

Typical usage (explicit free):
    client = TDTPClientDirect()
    pkt    = client.D_read("users.tdtp")
    try:
        out = client.D_filter(pkt, [{"field": "Balance", "op": "gt", "value": "1000"}])
        try:
            client.D_write(out, "filtered.tdtp")
        finally:
            out.free()
    finally:
        pkt.free()

Typical usage (context manager):
    with client.D_read_ctx("users.tdtp") as pkt:
        rows = pkt.get_rows()
"""
from __future__ import annotations

import ctypes
from contextlib import contextmanager
from typing import Generator

from tdtp._loader import lib
from tdtp._structs_d import D_FilterSpec, D_MaskConfig, D_Packet
from tdtp.exceptions import (
    TDTPFilterError,
    TDTPParseError,
    TDTPProcessorError,
    TDTPWriteError,
)


# ---------------------------------------------------------------------------
# Packet wrapper with auto-free support
# ---------------------------------------------------------------------------

class PacketHandle:
    """Wraps a D_Packet and its ctypes byref, providing free() and context manager.

    Do not instantiate directly; use TDTPClientDirect methods.
    """

    def __init__(self, pkt: D_Packet) -> None:
        self._pkt = pkt
        self._freed = False

    @property
    def pkt(self) -> D_Packet:
        if self._freed:
            raise RuntimeError("D_Packet already freed")
        return self._pkt

    def free(self) -> None:
        """Release C.malloc memory owned by this packet (idempotent)."""
        if not self._freed:
            lib.D_FreePacket(ctypes.byref(self._pkt))
            self._freed = True

    def get_rows(self) -> list[list[str]]:
        """Return all data rows as a list of string lists."""
        return self.pkt.get_rows()

    def get_schema(self) -> list[dict]:
        """Return schema field descriptors."""
        return self.pkt.get_schema()

    def to_pandas(self):
        """Convert this packet to a pandas DataFrame.

        Schema field names and types come from the D_* struct (lowercase keys
        ``"name"`` / ``"type"``); the conversion is handled by
        :func:`tdtp.pandas_ext.data_to_pandas`.

        Returns:
            ``pandas.DataFrame`` with columns named after schema fields and
            dtypes inferred from TDTP field types.

        Raises:
            ImportError: if pandas is not installed.
            RuntimeError: if the packet has already been freed.

        Example::

            client = TDTPClientDirect()
            with client.D_read_ctx("users.tdtp.xml") as pkt:
                df = pkt.to_pandas()
            print(df.describe())
        """
        from tdtp.pandas_ext import data_to_pandas

        # Build a J_read-compatible dict from D_* data.
        # D_Field.as_dict() uses lowercase keys ("name", "type"); wrap them
        # into uppercase keys so data_to_pandas works with both.
        raw_fields = self.get_schema()
        fields = [
            {"Name": f.get("name", ""), "Type": f.get("type", "TEXT")}
            for f in raw_fields
        ]
        data = {
            "schema": {"Fields": fields},
            "data":   self.get_rows(),
        }
        return data_to_pandas(data)

    def __enter__(self) -> "PacketHandle":
        return self

    def __exit__(self, *_) -> None:
        self.free()


# ---------------------------------------------------------------------------
# TDTPClientDirect
# ---------------------------------------------------------------------------

class TDTPClientDirect:
    """High-level Python client using the Direct boundary API (D_* exports).

    All data passes as C structs — no JSON serialization.
    The caller is responsible for freeing returned PacketHandle objects.
    """

    # -----------------------------------------------------------------------
    # I/O
    # -----------------------------------------------------------------------

    def D_read(self, path: str) -> PacketHandle:
        """Parse a .tdtp file and return a PacketHandle wrapping a D_Packet.

        Caller must call handle.free() when done (or use D_read_ctx).

        Raises:
            TDTPParseError: if the file cannot be parsed.
        """
        pkt = D_Packet()
        rc = lib.D_ReadFile(path.encode(), ctypes.byref(pkt))
        if rc != 0:
            raise TDTPParseError(pkt.get_error())
        return PacketHandle(pkt)

    @contextmanager
    def D_read_ctx(self, path: str) -> Generator[PacketHandle, None, None]:
        """Context manager version of D_read. Frees the packet on exit."""
        handle = self.D_read(path)
        try:
            yield handle
        finally:
            handle.free()

    def D_write(self, handle: PacketHandle, path: str) -> None:
        """Write a D_Packet to a .tdtp file.

        Raises:
            TDTPWriteError: if writing fails.
        """
        rc = lib.D_WriteFile(ctypes.byref(handle.pkt), path.encode())
        if rc != 0:
            raise TDTPWriteError(handle.pkt.get_error())

    # -----------------------------------------------------------------------
    # TDTQL filtering
    # -----------------------------------------------------------------------

    def D_filter(
        self,
        handle: PacketHandle,
        filters: list[dict],
        limit: int = 0,
    ) -> PacketHandle:
        """Filter rows by AND-combined filter specs.

        Args:
            handle:  source PacketHandle (not freed by this call).
            filters: list of dicts: [{"field": ..., "op": ..., "value": ..., "value2": ...}]
                     op values: eq|ne|gt|gte|lt|lte|in|not_in|between|
                                like|not_like|is_null|is_not_null
            limit:   max rows in result (0 = unlimited).

        Returns:
            New PacketHandle; caller must free.

        Raises:
            TDTPFilterError: if filter evaluation fails.
        """
        n = len(filters)
        if n > 0:
            arr = (D_FilterSpec * n)(*[D_FilterSpec.from_dict(f) for f in filters])
            filter_ptr = ctypes.cast(arr, ctypes.POINTER(D_FilterSpec))
        else:
            filter_ptr = ctypes.cast(None, ctypes.POINTER(D_FilterSpec))

        out = D_Packet()
        rc = lib.D_FilterRows(
            ctypes.byref(handle.pkt),
            filter_ptr,
            ctypes.c_int(n),
            ctypes.c_int(limit),
            ctypes.byref(out),
        )
        if rc != 0:
            raise TDTPFilterError(out.get_error())
        return PacketHandle(out)

    # -----------------------------------------------------------------------
    # Processors
    # -----------------------------------------------------------------------

    def D_apply_mask(
        self,
        handle: PacketHandle,
        fields: list[str],
        mask_char: str = "*",
        visible_chars: int = 4,
    ) -> PacketHandle:
        """Mask listed fields, returning a new PacketHandle.

        Args:
            handle:        source PacketHandle (not freed by this call).
            fields:        field names to mask, e.g. ["Email", "Phone"].
            mask_char:     replacement character (default "*").
            visible_chars: number of trailing chars to leave unmasked.

        Returns:
            New PacketHandle; caller must free.

        Raises:
            TDTPProcessorError: if masking fails.
        """
        cfg = D_MaskConfig.build(fields, mask_char, visible_chars)
        out = D_Packet()
        rc = lib.D_ApplyMask(
            ctypes.byref(handle.pkt),
            ctypes.byref(cfg),
            ctypes.byref(out),
        )
        lib.D_FreeMaskConfig(ctypes.byref(cfg))  # no-op; cfg owned by Python
        if rc != 0:
            raise TDTPProcessorError(out.get_error())
        return PacketHandle(out)

    def D_compress(self, handle: PacketHandle, level: int = 3) -> PacketHandle:
        """Compress data with zstd, returning a new PacketHandle.

        Args:
            handle: source PacketHandle (not freed by this call).
            level:  zstd compression level 1–22 (default 3).

        Returns:
            New PacketHandle with compression="zstd"; caller must free.

        Raises:
            TDTPProcessorError: if compression fails.
        """
        out = D_Packet()
        rc = lib.D_ApplyCompress(
            ctypes.byref(handle.pkt),
            ctypes.c_int(level),
            ctypes.byref(out),
        )
        if rc != 0:
            raise TDTPProcessorError(out.get_error())
        return PacketHandle(out)

    def D_decompress(self, handle: PacketHandle) -> PacketHandle:
        """Decompress a zstd-compressed packet, returning a new PacketHandle.

        Args:
            handle: source PacketHandle with compression="zstd" (not freed).

        Returns:
            New PacketHandle with plain rows; caller must free.

        Raises:
            TDTPProcessorError: if decompression fails.
        """
        out = D_Packet()
        rc = lib.D_ApplyDecompress(
            ctypes.byref(handle.pkt),
            ctypes.byref(out),
        )
        if rc != 0:
            raise TDTPProcessorError(out.get_error())
        return PacketHandle(out)
