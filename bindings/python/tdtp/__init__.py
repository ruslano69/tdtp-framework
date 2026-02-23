"""
tdtp — Python bindings for the TDTP framework.

Two API families, same Go core:

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
from tdtp.exceptions import (
    TDTPError,
    TDTPFilterError,
    TDTPLibraryError,
    TDTPParseError,
    TDTPProcessorError,
    TDTPWriteError,
)

__version__ = "0.1.0"
__all__ = [
    "TDTPClientJSON",
    "TDTPClientDirect",
    "PacketHandle",
    "TDTPError",
    "TDTPParseError",
    "TDTPFilterError",
    "TDTPProcessorError",
    "TDTPWriteError",
    "TDTPLibraryError",
]
