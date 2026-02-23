"""
ctypes Structure definitions mirroring the C structs declared in exports_d.go.

These are used by:
  - _loader.py  to configure D_* symbol signatures
  - api_d.py    to build / inspect D_Packet instances

Memory ownership: all pointer fields inside D_Packet are allocated by Go
(C.malloc). Always release with D_FreePacket via the lib handle.
D_MaskConfig.fields is owned by Python ctypes; D_FreeMaskConfig is a no-op.
"""
from __future__ import annotations

import ctypes


# ---------------------------------------------------------------------------
# D_Field — mirrors packet.Field
# ---------------------------------------------------------------------------

class D_Field(ctypes.Structure):
    """Single schema field descriptor."""
    _fields_ = [
        ("name",        ctypes.c_char * 256),
        ("type_name",   ctypes.c_char * 64),
        ("length",      ctypes.c_int),
        ("precision",   ctypes.c_int),
        ("scale",       ctypes.c_int),
        ("is_key",      ctypes.c_int),
        ("is_readonly", ctypes.c_int),
    ]

    def as_dict(self) -> dict:
        return {
            "name":      self.name.decode(errors="replace").rstrip("\x00"),
            "type":      self.type_name.decode(errors="replace").rstrip("\x00"),
            "length":    self.length,
            "precision": self.precision,
            "scale":     self.scale,
            "key":       bool(self.is_key),
            "readonly":  bool(self.is_readonly),
        }


# ---------------------------------------------------------------------------
# D_Schema — mirrors packet.Schema
# ---------------------------------------------------------------------------

class D_Schema(ctypes.Structure):
    """Array of D_Field descriptors."""
    _fields_ = [
        ("fields",      ctypes.POINTER(D_Field)),
        ("field_count", ctypes.c_int),
    ]

    def as_list(self) -> list[dict]:
        n = self.field_count
        if n <= 0 or not self.fields:
            return []
        return [self.fields[i].as_dict() for i in range(n)]


# ---------------------------------------------------------------------------
# D_Row — one parsed data row
# ---------------------------------------------------------------------------

class D_Row(ctypes.Structure):
    """Array of string values for a single data row."""
    _fields_ = [
        ("values",      ctypes.POINTER(ctypes.c_char_p)),
        ("value_count", ctypes.c_int),
    ]

    def as_list(self) -> list[str]:
        n = self.value_count
        if n <= 0 or not self.values:
            return []
        return [
            (self.values[i] or b"").decode(errors="replace")
            for i in range(n)
        ]


# ---------------------------------------------------------------------------
# D_Packet — primary result / argument struct
# ---------------------------------------------------------------------------

class D_Packet(ctypes.Structure):
    """Full TDTP data packet (schema + rows + metadata).

    Invariant: must be released via lib.D_FreePacket(ctypes.byref(pkt))
    after use. Do not share across threads without external synchronisation.
    """
    _fields_ = [
        ("rows",           ctypes.POINTER(D_Row)),
        ("row_count",      ctypes.c_int),
        ("schema",         D_Schema),
        ("msg_type",       ctypes.c_char * 32),
        ("table_name",     ctypes.c_char * 256),
        ("message_id",     ctypes.c_char * 64),
        ("timestamp_unix", ctypes.c_longlong),
        ("compression",    ctypes.c_char * 16),
        ("error",          ctypes.c_char * 1024),
    ]

    @property
    def has_error(self) -> bool:
        return bool(self.error and self.error != b"\x00")

    def get_error(self) -> str:
        return self.error.decode(errors="replace").rstrip("\x00")

    def get_rows(self) -> list[list[str]]:
        n = self.row_count
        if n <= 0 or not self.rows:
            return []
        return [self.rows[i].as_list() for i in range(n)]

    def get_schema(self) -> list[dict]:
        return self.schema.as_list()


# ---------------------------------------------------------------------------
# D_FilterSpec — one filter condition
# ---------------------------------------------------------------------------

class D_FilterSpec(ctypes.Structure):
    """A single WHERE condition.

    op values: eq | ne | gt | gte | lt | lte | in | not_in |
               between | like | not_like | is_null | is_not_null
    value2 is only used for 'between'.
    """
    _fields_ = [
        ("field",  ctypes.c_char * 256),
        ("op",     ctypes.c_char * 32),
        ("value",  ctypes.c_char * 1024),
        ("value2", ctypes.c_char * 1024),
    ]

    @classmethod
    def from_dict(cls, d: dict) -> "D_FilterSpec":
        spec = cls()
        spec.field  = d.get("field",  "").encode()[:255]
        spec.op     = d.get("op",     "eq").encode()[:31]
        spec.value  = d.get("value",  "").encode()[:1023]
        spec.value2 = d.get("value2", "").encode()[:1023]
        return spec


# ---------------------------------------------------------------------------
# D_MaskConfig — field masking configuration
# ---------------------------------------------------------------------------

class D_MaskConfig(ctypes.Structure):
    """Configuration for D_ApplyMask.

    Memory: fields array is owned by Python ctypes (not C.malloc).
    The Go side treats this as read-only; D_FreeMaskConfig is a no-op.
    Keep the instance alive until D_ApplyMask returns.
    """
    _fields_ = [
        ("fields",        ctypes.POINTER(ctypes.c_char_p)),
        ("field_count",   ctypes.c_int),
        ("mask_char",     ctypes.c_char * 4),
        ("visible_chars", ctypes.c_int),
    ]

    @classmethod
    def build(
        cls,
        fields: list[str],
        mask_char: str = "*",
        visible_chars: int = 4,
    ) -> "D_MaskConfig":
        cfg = cls()
        encoded = [f.encode() for f in fields]
        # Build a ctypes array of c_char_p (Python owns the memory).
        arr = (ctypes.c_char_p * len(encoded))(*encoded)
        cfg._arr_ref = arr        # prevent GC while cfg is alive
        cfg.fields = arr
        cfg.field_count = len(fields)
        cfg.mask_char = (mask_char[:1] or "*").encode()
        cfg.visible_chars = visible_chars
        return cfg
