"""
High-level facade — the recommended entry point for agents and scripts.

``Tdtp`` wraps the JSON boundary (:class:`~tdtp.api_j.TDTPClientJSON`) behind
plain-verb methods (``read``, ``filter``, ``merge``, ``verify`` …) with no
``J_`` prefix and no manual memory management. For maximum-throughput hot paths,
the typed Direct API is available as :attr:`Tdtp.direct`.

Quick start::

    from tdtp import Tdtp

    db = Tdtp()
    data = db.read("users.tdtp.xml")
    hot  = db.filter(data, "Balance > 1000", limit=100)
    db.write(hot, "rich.tdtp.xml")

    # integrity
    db.stamp(data, "signed.tdtp.xml")
    assert db.verify("signed.tdtp.xml")["ok"]

    # pandas in one step (requires `pip install tdtp[pandas]`)
    df = db.read_pandas("users.tdtp.xml", where="City = 'Omsk'")
"""
from __future__ import annotations

from typing import TYPE_CHECKING, Any

from tdtp.api_j import TDTPClientJSON

if TYPE_CHECKING:
    import pandas as pd

    from tdtp.api_d import TDTPClientDirect


class Tdtp:
    """One-stop TDTP client with plain-verb methods over the JSON boundary.

    Stateless and thread-safe: a single instance can be shared across threads.
    """

    def __init__(self) -> None:
        self._j = TDTPClientJSON()
        self._d: "TDTPClientDirect | None" = None

    # -- introspection -------------------------------------------------------

    @property
    def version(self) -> str:
        """Native library version (matches the compiled Go core)."""
        return self._j.J_get_version()

    @property
    def json(self) -> TDTPClientJSON:
        """The underlying JSON-boundary client (low-level escape hatch)."""
        return self._j

    @property
    def direct(self) -> "TDTPClientDirect":
        """The Direct (C-struct) client for maximum-throughput hot paths.

        Lazily constructed on first access. Use its ``D_read_ctx`` context
        manager to read large packets with explicit, scoped memory management.
        """
        if self._d is None:
            from tdtp.api_d import TDTPClientDirect
            self._d = TDTPClientDirect()
        return self._d

    # -- I/O -----------------------------------------------------------------

    def read(self, path: str) -> dict:
        """Read a ``.tdtp`` file into a dict (transparently decompresses)."""
        return self._j.J_read(path)

    def read_multipart(self, path: str) -> dict:
        """Assemble a ``_part_N_of_M`` batch into one dataset (pass any part)."""
        return self._j.J_read_multipart(path)

    def write(self, data: dict, path: str) -> None:
        """Write a dataset dict to a ``.tdtp`` file."""
        self._j.J_write(data, path)

    def export(self, data: dict, base_path: str, **opts: Any) -> dict:
        """Partition + write a dataset (optionally compress / compact).

        Keyword options: ``compress``, ``algo``, ``level``, ``checksum``,
        ``compact``, ``fixed_fields``, ``compact_tail`` — see
        :meth:`TDTPClientJSON.J_export_all`.
        """
        return self._j.J_export_all(data, base_path, **opts)

    # -- inspection / integrity ---------------------------------------------

    def inspect(self, path: str) -> dict:
        """Structured packet metadata without decompressing the data section."""
        return self._j.J_inspect(path)

    def test(self, path: str) -> dict:
        """Dry-run integrity check (parts present, checksum, decompress, rows)."""
        return self._j.J_test(path)

    def verify(self, path: str) -> dict:
        """Verify v1.4 XXH3 integrity hashes (local, no Mercury)."""
        return self._j.J_verify(path)

    def stamp(self, data: dict, path: str) -> dict:
        """Compute XXH3 hashes and write a stamped v1.4 file; returns fingerprints."""
        return self._j.J_stamp(data, path)

    # -- query / transform ---------------------------------------------------

    def filter(self, data: dict, where: str, limit: int = 0, offset: int = 0) -> dict:
        """Filter rows with a TDTQL WHERE clause (paginated when limit > 0)."""
        return self._j.J_filter(data, where, limit=limit, offset=offset)

    def sort(self, data: dict, order_by: list[dict] | str) -> dict:
        """Sort rows by one field (str) or several keys (list of dicts)."""
        return self._j.J_sort(data, order_by)

    def merge(
        self,
        packets: list[dict],
        strategy: str = "union",
        key_fields: list[str] | None = None,
    ) -> dict:
        """Merge datasets (union/intersection/left/right/append)."""
        return self._j.J_merge(packets, strategy=strategy, key_fields=key_fields)

    def diff(self, old: dict, new: dict) -> dict:
        """Row-level diff between two datasets (added/removed/modified/stats)."""
        return self._j.J_diff(old, new)

    def apply_processor(self, data: dict, proc_type: str, **config: Any) -> dict:
        """Run a single processor (mask/normalize/validate/compress)."""
        return self._j.J_apply_processor(data, proc_type, **config)

    def apply_chain(self, data: dict, chain: list[dict]) -> dict:
        """Run an ordered chain of processors."""
        return self._j.J_apply_chain(data, chain)

    # -- pandas (optional) ---------------------------------------------------

    def to_pandas(self, data: dict) -> "pd.DataFrame":
        """Convert a dataset dict to a pandas DataFrame (requires pandas)."""
        return self._j.J_to_pandas(data)

    def from_pandas(self, df: "pd.DataFrame", table_name: str = "data") -> dict:
        """Convert a pandas DataFrame to a dataset dict (requires pandas)."""
        return self._j.J_from_pandas(df, table_name=table_name)

    def read_pandas(self, path: str, where: str = "", limit: int = 0) -> "pd.DataFrame":
        """Read a file straight into a DataFrame, optionally filtered."""
        return self._j.read_pandas(path, where=where, limit=limit)

    def write_pandas(self, df: "pd.DataFrame", path: str, table_name: str = "") -> None:
        """Write a DataFrame to a ``.tdtp`` file."""
        self._j.write_pandas(df, path, table_name=table_name)

    def __repr__(self) -> str:
        return f"<Tdtp version={self.version!r}>"
