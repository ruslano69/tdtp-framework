"""
tdtp — Python bindings for the TDTP framework.

Recommended entry point — the Tdtp facade (plain verbs, no manual free):
    from tdtp import Tdtp
    db   = Tdtp()
    data = db.read("users.tdtp")
    hot  = db.filter(data, "Balance > 1000", limit=100)
    db.write(hot, "rich.tdtp")

    db.stamp(data, "signed.tdtp")           # write a v1.4 integrity packet
    assert db.verify("signed.tdtp")["ok"]   # confirm it was not tampered

Underlying it are two API families over the same Go core:

    J_* (JSON boundary)   — TDTPClientJSON
        Data serialized as JSON at the language boundary.
        Universal, simple memory model (no manual free), small overhead.

    D_* (Direct boundary) — TDTPClientDirect
        Data passed as C structs via ctypes pointers.
        Maximum throughput, explicit memory management (PacketHandle.free()).

Quick start — JSON API:
    from tdtp import TDTPClientJSON
    client = TDTPClientJSON()
    data   = client.J_read("users.tdtp")
    result = client.J_filter(data, "Balance > 1000", limit=100)

Quick start — Direct API:
    from tdtp import TDTPClientDirect
    client = TDTPClientDirect()
    with client.D_read_ctx("users.tdtp") as pkt:
        rows = pkt.get_rows()
"""
from tdtp.api_d import TDTPClientDirect, PacketHandle
from tdtp.api_j import TDTPClientJSON
from tdtp.facade import Tdtp
from tdtp.exceptions import (
    TDTPEncryptedPacketError,
    TDTPError,
    TDTPFilterError,
    TDTPLibraryError,
    TDTPParseError,
    TDTPProcessorError,
    TDTPWriteError,
)

# Optional pandas helpers — imported lazily so pandas is not required
try:
    from tdtp.pandas_ext import data_to_pandas, pandas_to_data
    _PANDAS_AVAILABLE = True
except ImportError:
    _PANDAS_AVAILABLE = False

# Version is sourced from the native library (J_GetVersion → pkg/core/version.Version),
# keeping the Python package version in lockstep with the compiled Go core.
# Falls back to "unknown" if the library cannot be queried at import time.
try:
    from tdtp._loader import get_lib_version as _get_lib_version
    __version__ = _get_lib_version()
except Exception:  # pragma: no cover - library load failure path
    __version__ = "unknown"


def _check_version_lockstep() -> None:
    """Warn if the loaded .so and the installed package metadata disagree.

    The native library is the runtime source of truth (J_GetVersion), but the
    wheel/sdist also carries a static version in its metadata (from
    pyproject.toml). They can drift if a stale .so ships with a newer package
    or vice versa. ``make build-lib`` runs ``sync-version`` to prevent this at
    build time; this guard catches a mismatch that slipped through at runtime.
    """
    if __version__ == "unknown":
        return
    try:
        from importlib.metadata import PackageNotFoundError, version as _pkg_version
    except ImportError:  # pragma: no cover - Python < 3.8
        return
    try:
        pkg_ver = _pkg_version("tdtp")
    except PackageNotFoundError:  # pragma: no cover - running from source, not installed
        return
    if pkg_ver != __version__:
        import warnings
        warnings.warn(
            f"tdtp version mismatch: native library reports {__version__!r} but "
            f"the installed package metadata is {pkg_ver!r}. The compiled "
            f"libtdtp is out of sync with the package — rebuild it with "
            f"`make build-lib` (or `make build-lib-full`).",
            RuntimeWarning,
            stacklevel=2,
        )


_check_version_lockstep()

__all__ = [
    "Tdtp",
    "TDTPClientJSON",
    "TDTPClientDirect",
    "PacketHandle",
    "TDTPError",
    "TDTPParseError",
    "TDTPEncryptedPacketError",
    "TDTPFilterError",
    "TDTPProcessorError",
    "TDTPWriteError",
    "TDTPLibraryError",
    # pandas helpers (available only when pandas is installed)
    "data_to_pandas",
    "pandas_to_data",
]
