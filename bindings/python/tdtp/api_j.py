"""
JSON boundary API — TDTPClientJSON.

All data crosses the Python↔Go boundary as UTF-8 JSON strings.
Small serialization overhead; works from any language that can call C and parse JSON.

Typical usage:
    from tdtp import TDTPClientJSON

    client = TDTPClientJSON()
    data   = client.J_read("users.tdtp")
    result = client.J_filter(data, "Balance > 1000 AND City = 'Omsk'", limit=200)
    client.J_write(result, "filtered.tdtp")
"""
from __future__ import annotations

import ctypes
import json
from typing import Any

from tdtp._loader import lib, free_string
from tdtp.exceptions import (
    TDTPFilterError,
    TDTPParseError,
    TDTPProcessorError,
    TDTPWriteError,
)


# ---------------------------------------------------------------------------
# Internal helpers
# ---------------------------------------------------------------------------

def _call(fn, *args) -> dict:
    """Call a J_* function, decode the JSON result, free the C string.

    Raises TDTPError subclass if the result contains {"error": "..."}.
    """
    # TODO: ptr = fn(*args)
    # TODO: raw = ctypes.string_at(ptr)
    # TODO: free_string(ptr)
    # TODO: result = json.loads(raw)
    # TODO: if "error" in result and result["error"]: raise appropriate exception
    # TODO: return result
    raise NotImplementedError


# ---------------------------------------------------------------------------
# TDTPClientJSON
# ---------------------------------------------------------------------------

class TDTPClientJSON:
    """High-level Python client using the JSON boundary API (J_* exports).

    Thread safety: instances are stateless; the same instance can be shared
    across threads. Calls to libtdtp.so functions are serialized by the GIL
    during the ctypes call itself.
    """

    # -----------------------------------------------------------------------
    # Version
    # -----------------------------------------------------------------------

    def J_get_version(self) -> str:
        """Return the native library version string."""
        # TODO: ptr = lib.J_GetVersion()
        # TODO: version = ctypes.string_at(ptr).decode()
        # TODO: free_string(ptr)
        # TODO: return version
        raise NotImplementedError

    # -----------------------------------------------------------------------
    # I/O
    # -----------------------------------------------------------------------

    def J_read(self, path: str) -> dict:
        """Parse a .tdtp file and return its contents as a Python dict.

        Returns:
            {
                "schema": {"fields": [{"name": ..., "type": ..., ...}]},
                "header": {"type": ..., "table_name": ..., ...},
                "data":   [["v1", "v2", ...], ...],
            }

        Raises:
            TDTPParseError: if the file cannot be parsed.
        """
        # TODO: return _call(lib.J_ReadFile, path.encode())
        raise NotImplementedError

    def J_write(self, data: dict, path: str) -> None:
        """Generate a .tdtp file from data dict and write it to path.

        Args:
            data: dict in the shape returned by J_read.
            path: destination file path.

        Raises:
            TDTPWriteError: if writing fails.
        """
        # TODO: _call(lib.J_WriteFile, json.dumps(data).encode(), path.encode())
        raise NotImplementedError

    # -----------------------------------------------------------------------
    # TDTQL filtering
    # -----------------------------------------------------------------------

    def J_filter(
        self,
        data: dict,
        where: str,
        limit: int = 0,
    ) -> dict:
        """Filter data rows using a TDTQL WHERE clause.

        Args:
            data:  dict in the shape returned by J_read.
            where: TDTQL expression, e.g. "Balance > 1000 AND City = 'Omsk'".
            limit: maximum rows to return (0 = unlimited).

        Returns:
            Same shape as J_read with filtered rows.

        Raises:
            TDTPFilterError: if the WHERE clause is invalid.
        """
        # TODO: return _call(lib.J_FilterRows,
        #                    json.dumps(data).encode(),
        #                    where.encode(),
        #                    ctypes.c_int(limit))
        raise NotImplementedError

    # -----------------------------------------------------------------------
    # Processors
    # -----------------------------------------------------------------------

    def J_apply_processor(
        self,
        data: dict,
        proc_type: str,
        **config: Any,
    ) -> dict:
        """Run a single named processor over data.

        Args:
            data:      dict in the shape returned by J_read.
            proc_type: one of "field_masker" | "field_normalizer" |
                       "field_validator" | "checksum" | "compress" | "decompress".
            **config:  processor-specific keyword arguments, e.g.:
                       field_masker  → fields=["email"], mask_char="*", visible_chars=4
                       compress      → algorithm="zstd", level=3

        Returns:
            Same shape as J_read with processed data.

        Raises:
            TDTPProcessorError: if the processor fails.
        """
        # TODO: return _call(lib.J_ApplyProcessor,
        #                    json.dumps(data).encode(),
        #                    proc_type.encode(),
        #                    json.dumps(config).encode())
        raise NotImplementedError

    def J_apply_chain(
        self,
        data: dict,
        chain: list[dict],
    ) -> dict:
        """Run an ordered chain of processors over data.

        Args:
            data:  dict in the shape returned by J_read.
            chain: list of processor configs:
                   [{"type": "field_masker", "params": {...}},
                    {"type": "compress",     "params": {"level": 3}}]

        Returns:
            Same shape as J_read with processed data.

        Raises:
            TDTPProcessorError: if any processor in the chain fails.
        """
        # TODO: return _call(lib.J_ApplyChain,
        #                    json.dumps(data).encode(),
        #                    json.dumps(chain).encode())
        raise NotImplementedError

    # -----------------------------------------------------------------------
    # Diff
    # -----------------------------------------------------------------------

    def J_diff(self, old: dict, new: dict) -> dict:
        """Compute the difference between two TDTP datasets.

        Args:
            old: baseline dataset (shape returned by J_read).
            new: updated dataset  (shape returned by J_read).

        Returns:
            {
                "added":    [["v1", ...], ...],
                "removed":  [["v1", ...], ...],
                "modified": [{"key": ..., "changes": {...}}, ...],
                "stats":    {"added": N, "removed": N, "modified": N},
            }
        """
        # TODO: return _call(lib.J_Diff,
        #                    json.dumps(old).encode(),
        #                    json.dumps(new).encode())
        raise NotImplementedError
