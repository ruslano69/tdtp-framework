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
    TDTPError,
    TDTPFilterError,
    TDTPParseError,
    TDTPProcessorError,
    TDTPWriteError,
)

# Map error message prefixes → specific exception types
_ERROR_MAP: list[tuple[str, type[TDTPError]]] = [
    ("parse error",          TDTPParseError),
    ("decompress error",     TDTPParseError),
    ("invalid WHERE clause", TDTPFilterError),
    ("filter error",         TDTPFilterError),
    ("processor error",      TDTPProcessorError),
    ("process error",        TDTPProcessorError),
    ("chain",                TDTPProcessorError),
    ("write error",          TDTPWriteError),
]


# ---------------------------------------------------------------------------
# Internal helpers
# ---------------------------------------------------------------------------

def _call(fn, *args) -> dict:
    """Call a J_* function, decode the JSON result, free the C string.

    All J_* functions have restype=c_void_p, so raw_ptr is an integer address.
    We read the bytes with ctypes.string_at(), then free via free_string().
    Raises the appropriate TDTPError subclass when result contains {"error":"..."}.
    """
    raw_ptr = fn(*args)  # integer address (c_void_p)
    if not raw_ptr:
        raise TDTPError("J_* function returned NULL pointer")

    raw_bytes = ctypes.string_at(raw_ptr)   # read C string by address
    free_string(raw_ptr)                     # release Go-allocated memory

    result = json.loads(raw_bytes)

    err_msg = result.get("error", "")
    if err_msg:
        exc_type = TDTPError
        for prefix, exc_cls in _ERROR_MAP:
            if err_msg.lower().startswith(prefix.lower()):
                exc_type = exc_cls
                break
        raise exc_type(err_msg)

    return result


# ---------------------------------------------------------------------------
# TDTPClientJSON
# ---------------------------------------------------------------------------

class TDTPClientJSON:
    """High-level Python client using the JSON boundary API (J_* exports).

    Thread safety: instances are stateless; the same instance can be shared
    across threads. The GIL serialises ctypes calls naturally.
    """

    # -----------------------------------------------------------------------
    # Version
    # -----------------------------------------------------------------------

    def J_get_version(self) -> str:
        """Return the native library version string, e.g. '1.6.0'."""
        ptr = lib.J_GetVersion()
        version = ctypes.string_at(ptr).decode()
        free_string(ptr)
        return version

    # -----------------------------------------------------------------------
    # I/O
    # -----------------------------------------------------------------------

    def J_read(self, path: str) -> dict:
        """Parse a .tdtp file and return its contents as a Python dict.

        Returns::

            {
                "schema": {"fields": [{"name": ..., "type": ..., ...}]},
                "header": {"type": ..., "table_name": ..., "timestamp": ..., ...},
                "data":   [["v1", "v2", ...], ...],
            }

        Args:
            path: path to the .tdtp (XML) file.

        Raises:
            TDTPParseError: if the file cannot be parsed or decompressed.
        """
        return _call(lib.J_ReadFile, path.encode())

    def J_write(self, data: dict, path: str) -> None:
        """Generate a .tdtp file from a data dict and write it to path.

        Args:
            data: dict in the shape returned by :meth:`J_read`.
            path: destination file path.

        Raises:
            TDTPWriteError: if writing fails.
        """
        _call(lib.J_WriteFile, json.dumps(data).encode(), path.encode())

    def J_export_all(
        self,
        data: dict,
        base_path: str,
        compress: bool = False,
        level: int = 3,
        checksum: bool = True,
    ) -> dict:
        """Partition data and write all parts using the framework's native byte-size logic.

        Mirrors tdtpcli behaviour: data is split into ~3.8 MB parts automatically
        (same ``generator.GenerateReference`` logic), with optional zstd compression
        and XXH3 checksums applied to each part before writing.

        Args:
            data:      Full dataset dict (schema + header + data rows).
            base_path: Output base path, e.g. ``"/tmp/out/Users.tdtp.xml"``.
                       Multiple parts are written as ``Users_part_1_of_N.tdtp.xml``.
            compress:  Apply zstd compression (requires libtdtp built with
                       ``-tags compress``).
            level:     zstd compression level 1–19 (default 3).
            checksum:  Compute XXH3 checksum when compressing (default True).

        Returns:
            ``{"files": [...], "total_parts": N}``

        Raises:
            TDTPError: if partitioning, compression, or writing fails.

        Example::

            import pandas as pd
            df   = pd.read_sql_query("SELECT * FROM Users", conn)
            data = client.J_from_pandas(df, table_name="Users")
            result = client.J_export_all(
                data, "/tmp/export/Users.tdtp.xml",
                compress=True, checksum=True,
            )
            print(result["total_parts"], "files written")
        """
        opts = {"compress": compress, "level": level, "checksum": checksum}
        return _call(
            lib.J_ExportAll,
            json.dumps(data).encode(),
            base_path.encode(),
            json.dumps(opts).encode(),
        )

    # -----------------------------------------------------------------------
    # TDTQL filtering
    # -----------------------------------------------------------------------

    def J_filter(
        self,
        data: dict,
        where: str,
        limit: int = 0,
        offset: int = 0,
    ) -> dict:
        """Filter data rows using a TDTQL WHERE clause with optional pagination.

        Uses the framework-native ``executor.Execute`` path, so LIMIT and OFFSET
        are applied inside the Go core rather than via Python-level slicing.

        Args:
            data:   dict in the shape returned by :meth:`J_read`.
            where:  TDTQL expression, e.g. ``"Balance > 1000 AND City = 'Omsk'"``.
            limit:  maximum rows to return per page (0 = unlimited).
            offset: number of matched rows to skip before returning results (default 0).

        Returns:
            dict with the same ``"schema"`` / ``"header"`` / ``"data"`` keys as
            :meth:`J_read`, plus an optional ``"query_context"`` object when
            ``limit > 0``::

                {
                    "schema": ...,
                    "header": ...,
                    "data":   [...],
                    "query_context": {
                        "total_records":    <int>,
                        "matched_records":  <int>,
                        "returned_records": <int>,
                        "more_available":   <bool>,
                        "next_offset":      <int>,   # only when more_available is True
                        "limit":            <int>,
                        "offset":           <int>,
                    }
                }

        Raises:
            TDTPFilterError: if the WHERE clause is invalid or evaluation fails.
        """
        return _call(
            lib.J_FilterRowsPage,
            json.dumps(data).encode(),
            where.encode(),
            ctypes.c_int(limit),
            ctypes.c_int(offset),
        )

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
            data:      dict in the shape returned by :meth:`J_read`.
            proc_type: one of ``"field_masker"`` | ``"field_normalizer"`` |
                       ``"field_validator"`` | ``"compress"`` | ``"decompress"``.
            **config:  processor-specific keyword arguments, e.g.:

                       * ``field_masker``  → ``fields=["email"], mask_char="*", visible_chars=4``
                       * ``compress``      → ``level=3``

        Returns:
            Same shape as :meth:`J_read` with processed data.

        Raises:
            TDTPProcessorError: if the processor fails.

        Note:
            ``compress`` and ``decompress`` require libtdtp built with
            ``-tags compress`` (i.e. ``make build-lib-full``).
        """
        return _call(
            lib.J_ApplyProcessor,
            json.dumps(data).encode(),
            proc_type.encode(),
            json.dumps(config).encode(),
        )

    def J_apply_chain(
        self,
        data: dict,
        chain: list[dict],
    ) -> dict:
        """Run an ordered chain of processors over data.

        Args:
            data:  dict in the shape returned by :meth:`J_read`.
            chain: list of processor configs::

                       [{"type": "field_masker", "params": {"fields": ["email"]}},
                        {"type": "compress",     "params": {"level": 3}}]

        Returns:
            Same shape as :meth:`J_read` with processed data.

        Raises:
            TDTPProcessorError: if any processor in the chain fails.
        """
        return _call(
            lib.J_ApplyChain,
            json.dumps(data).encode(),
            json.dumps(chain).encode(),
        )

    # -----------------------------------------------------------------------
    # Diff
    # -----------------------------------------------------------------------

    # -----------------------------------------------------------------------
    # Pandas integration (optional — requires pandas)
    # -----------------------------------------------------------------------

    def J_to_pandas(self, data: dict):
        """Convert a J_read result dict to a pandas DataFrame.

        Delegates to :func:`tdtp.pandas_ext.data_to_pandas`.

        Args:
            data: dict returned by :meth:`J_read` (or :meth:`J_filter`, etc.).

        Returns:
            ``pandas.DataFrame`` with columns named after schema fields and
            dtypes inferred from TDTP field types.

        Raises:
            ImportError: if pandas is not installed.

        Example::

            client = TDTPClientJSON()
            raw    = client.J_read("users.tdtp.xml")
            df     = client.J_to_pandas(raw)
            print(df.describe())
            df.to_csv("users.csv", index=False)
        """
        from tdtp.pandas_ext import data_to_pandas
        return data_to_pandas(data)

    def J_from_pandas(self, df, table_name: str = "data", message_id: str = "") -> dict:
        """Convert a pandas DataFrame to a TDTP data dict.

        The returned dict can be passed directly to :meth:`J_write`.

        Args:
            df:         ``pandas.DataFrame`` to convert.
            table_name: table name written into the TDTP header.
            message_id: TDTP ``MessageID`` (required by the parser). If empty,
                        a UUID4 is generated automatically.

        Returns:
            dict with ``"schema"``, ``"header"``, and ``"data"`` keys.

        Raises:
            ImportError: if pandas is not installed.

        Example::

            import pandas as pd
            client = TDTPClientJSON()
            df     = pd.read_csv("input.csv")
            data   = client.J_from_pandas(df, table_name="users")
            client.J_write(data, "output.tdtp.xml")
        """
        from tdtp.pandas_ext import pandas_to_data
        return pandas_to_data(df, table_name=table_name, message_id=message_id)

    # -----------------------------------------------------------------------
    # Diff
    # -----------------------------------------------------------------------

    def J_diff(self, old: dict, new: dict) -> dict:
        """Compute the row-level difference between two TDTP datasets.

        Args:
            old: baseline dataset (shape returned by :meth:`J_read`).
            new: updated dataset  (shape returned by :meth:`J_read`).

        Returns::

            {
                "added":    [["v1", ...], ...],
                "removed":  [["v1", ...], ...],
                "modified": [{"key": ..., "old_row": [...], "new_row": [...],
                              "changes": {<field_idx>: {"field_name": ...,
                                          "old_value": ..., "new_value": ...}}},
                             ...],
                "stats": {"total_in_a": N, "total_in_b": N,
                          "added": N, "removed": N, "modified": N, "unchanged": N},
            }
        """
        return _call(
            lib.J_Diff,
            json.dumps(old).encode(),
            json.dumps(new).encode(),
        )
