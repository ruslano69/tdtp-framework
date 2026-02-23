"""
ctypes Structure definitions mirroring the C structs declared in exports_d.go.

These are used by:
  - _loader.py  to configure D_* symbol signatures
  - api_d.py    to build / inspect D_Packet instances

Memory ownership: all pointer fields inside D_Packet / D_MaskConfig are
allocated by Go (C.malloc). Always release with D_FreePacket / D_FreeMaskConfig
via the lib handle — never call ctypes.free() directly.
"""
from __future__ import annotations

import ctypes


# ---------------------------------------------------------------------------
# D_Field — mirrors packet.Field
# ---------------------------------------------------------------------------

class D_Field(ctypes.Structure):
    """Single schema field descriptor."""
    _fields_ = [
        ("name",       ctypes.c_char * 256),
        ("type_name",  ctypes.c_char * 64),   # maps to packet.Field.Type
        ("length",     ctypes.c_int),
        ("precision",  ctypes.c_int),
        ("scale",      ctypes.c_int),
        ("is_key",     ctypes.c_int),          # 0 / 1
        ("is_readonly", ctypes.c_int),         # 0 / 1
    ]

    def as_dict(self) -> dict:
        # TODO: return {name, type, length, precision, scale, key, readonly}
        raise NotImplementedError


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
        # TODO: iterate self.fields[:self.field_count] → [field.as_dict(), ...]
        raise NotImplementedError


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
        # TODO: return [self.values[i].decode() for i in range(self.value_count)]
        raise NotImplementedError


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
        # TODO: return [self.rows[i].as_list() for i in range(self.row_count)]
        raise NotImplementedError

    def get_schema(self) -> list[dict]:
        # TODO: return self.schema.as_list()
        raise NotImplementedError


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
        # TODO: build D_FilterSpec from {"field":..., "op":..., "value":..., "value2":...}
        raise NotImplementedError


# ---------------------------------------------------------------------------
# D_MaskConfig — field masking configuration
# ---------------------------------------------------------------------------

class D_MaskConfig(ctypes.Structure):
    """Configuration for D_ApplyMask."""
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
        # TODO: C.malloc fields array, fill with encoded strings
        # TODO: set mask_char and visible_chars
        # TODO: caller is responsible for D_FreeMaskConfig
        raise NotImplementedError
