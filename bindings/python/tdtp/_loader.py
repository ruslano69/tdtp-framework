"""
Native library loader.

Locates libtdtp.so (or .dll / .dylib) relative to this package,
configures ctypes argtypes / restype for every exported symbol,
and exposes a module-level `lib` instance.

Usage (internal):
    from tdtp._loader import lib, free_string
"""
from __future__ import annotations

import ctypes
import os
import platform
from pathlib import Path

from tdtp.exceptions import TDTPLibraryError

# ---------------------------------------------------------------------------
# Locate shared library
# ---------------------------------------------------------------------------

def _lib_name() -> str:
    system = platform.system()
    if system == "Windows":
        return "libtdtp.dll"
    if system == "Darwin":
        return "libtdtp.dylib"
    return "libtdtp.so"


def _find_library() -> Path:
    """Return the path to the platform-specific libtdtp shared library.

    Search order:
      1. TDTP_LIB_PATH environment variable (absolute path to .so/.dll/.dylib)
      2. Directory of this package   (installed wheel bundles the .so alongside)
      3. Repository root build output (development: ``make build-lib``)
    """
    name = _lib_name()

    # 1. Explicit override
    env_path = os.environ.get("TDTP_LIB_PATH")
    if env_path:
        p = Path(env_path)
        if p.exists():
            return p
        raise TDTPLibraryError(
            f"TDTP_LIB_PATH={env_path!r} does not exist"
        )

    # 2. Alongside the package (installed wheel / make build-lib)
    pkg_dir = Path(__file__).parent
    candidate = pkg_dir / name
    if candidate.exists():
        return candidate

    # 3. Parent bindings dir (running from source tree)
    for parent in pkg_dir.parents:
        candidate = parent / "tdtp" / name
        if candidate.exists():
            return candidate

    raise TDTPLibraryError(
        f"Cannot find {name}. Run 'make build-lib' in bindings/python/ "
        f"or set TDTP_LIB_PATH to the .so path."
    )


# ---------------------------------------------------------------------------
# Library loading + symbol configuration
# ---------------------------------------------------------------------------

def _load(lib_path: Path) -> ctypes.CDLL:
    """Load the shared library and configure all symbol signatures."""
    try:
        _lib = ctypes.CDLL(str(lib_path))
    except OSError as exc:
        raise TDTPLibraryError(f"Cannot load {lib_path}: {exc}") from exc

    _configure_j_symbols(_lib)
    _configure_d_symbols(_lib)
    return _lib


def _configure_j_symbols(lib: ctypes.CDLL) -> None:
    """Set argtypes and restype for all J_* exported functions.

    NOTE: All functions that return *C.char use c_void_p (not c_char_p).
    c_char_p automatically converts the pointer to Python bytes, losing the
    original address and making J_FreeString impossible to call.
    We read the string with ctypes.string_at(ptr) and free via free_string().
    """
    # J_GetVersion() → *char  (we own the pointer; must J_FreeString it)
    lib.J_GetVersion.argtypes = []
    lib.J_GetVersion.restype = ctypes.c_void_p

    # J_FreeString(*char) → void
    lib.J_FreeString.argtypes = [ctypes.c_void_p]
    lib.J_FreeString.restype = None

    # J_ReadFile(*char) → *char
    lib.J_ReadFile.argtypes = [ctypes.c_char_p]
    lib.J_ReadFile.restype = ctypes.c_void_p

    # J_ParseBytes(*char, c_int) → *char  (in-memory counterpart of J_ReadFile;
    # parses a TDTP blob already in memory, no filesystem access)
    lib.J_ParseBytes.argtypes = [ctypes.c_char_p, ctypes.c_int]
    lib.J_ParseBytes.restype = ctypes.c_void_p

    # J_Inspect(*char) → *char  (structured metadata, no decompression)
    lib.J_Inspect.argtypes = [ctypes.c_char_p]
    lib.J_Inspect.restype = ctypes.c_void_p

    # J_InspectBytes(*char, c_int) → *char  (in-memory counterpart of J_Inspect)
    lib.J_InspectBytes.argtypes = [ctypes.c_char_p, ctypes.c_int]
    lib.J_InspectBytes.restype = ctypes.c_void_p

    # J_WriteColumnar(*char, *char) → *char  (column-major write; avoids
    # Python-side row transposition for numpy/pyarrow column data)
    lib.J_WriteColumnar.argtypes = [ctypes.c_char_p, ctypes.c_char_p]
    lib.J_WriteColumnar.restype = ctypes.c_void_p

    # J_ReadMultipart(*char) → *char  (assemble _part_N_of_M batch into one dataset)
    lib.J_ReadMultipart.argtypes = [ctypes.c_char_p]
    lib.J_ReadMultipart.restype = ctypes.c_void_p

    # J_Test(*char) → *char  (dry-run integrity check, no DB)
    lib.J_Test.argtypes = [ctypes.c_char_p]
    lib.J_Test.restype = ctypes.c_void_p

    # J_Verify(*char) → *char  (v1.4 XXH3 integrity verification, no Mercury)
    lib.J_Verify.argtypes = [ctypes.c_char_p]
    lib.J_Verify.restype = ctypes.c_void_p

    # J_VerifyMercury(*char, *char) → *char  (v1.4 verification against a live
    # xzMercury hash registry — the network-verifying counterpart of J_Verify)
    lib.J_VerifyMercury.argtypes = [ctypes.c_char_p, ctypes.c_char_p]
    lib.J_VerifyMercury.restype = ctypes.c_void_p

    # J_Stamp(*char, *char) → *char  (compute XXH3 hashes, write v1.4 file)
    lib.J_Stamp.argtypes = [ctypes.c_char_p, ctypes.c_char_p]
    lib.J_Stamp.restype = ctypes.c_void_p

    # J_WriteFile(*char, *char) → *char
    lib.J_WriteFile.argtypes = [ctypes.c_char_p, ctypes.c_char_p]
    lib.J_WriteFile.restype = ctypes.c_void_p

    # J_FilterRows(*char, *char, c_int) → *char
    lib.J_FilterRows.argtypes = [ctypes.c_char_p, ctypes.c_char_p, ctypes.c_int]
    lib.J_FilterRows.restype = ctypes.c_void_p

    # J_FilterRowsPage(*char, *char, c_int, c_int) → *char
    # Returns schema/header/data + "query_context" pagination metadata.
    lib.J_FilterRowsPage.argtypes = [
        ctypes.c_char_p, ctypes.c_char_p, ctypes.c_int, ctypes.c_int,
    ]
    lib.J_FilterRowsPage.restype = ctypes.c_void_p

    # J_ApplyProcessor(*char, *char, *char) → *char
    lib.J_ApplyProcessor.argtypes = [
        ctypes.c_char_p, ctypes.c_char_p, ctypes.c_char_p,
    ]
    lib.J_ApplyProcessor.restype = ctypes.c_void_p

    # J_ApplyChain(*char, *char) → *char
    lib.J_ApplyChain.argtypes = [ctypes.c_char_p, ctypes.c_char_p]
    lib.J_ApplyChain.restype = ctypes.c_void_p

    # J_ExportAll(*char, *char, *char) → *char
    lib.J_ExportAll.argtypes = [ctypes.c_char_p, ctypes.c_char_p, ctypes.c_char_p]
    lib.J_ExportAll.restype = ctypes.c_void_p

    # J_Diff(*char, *char) → *char
    lib.J_Diff.argtypes = [ctypes.c_char_p, ctypes.c_char_p]
    lib.J_Diff.restype = ctypes.c_void_p

    # J_Sort(*char, *char) → *char  (data, order-by spec)
    lib.J_Sort.argtypes = [ctypes.c_char_p, ctypes.c_char_p]
    lib.J_Sort.restype = ctypes.c_void_p

    # J_Merge(*char, *char) → *char  (packets array, options)
    lib.J_Merge.argtypes = [ctypes.c_char_p, ctypes.c_char_p]
    lib.J_Merge.restype = ctypes.c_void_p

    # J_SerializeValue(*char, *char) → *char
    # Single source of truth for type serialization in Python adapters.
    # tdtpType: "BLOB" | "TIMESTAMP" | "DATETIME" | "JSON" | "JSONB" | …
    # value:    raw value as string (BLOB=hex, TIMESTAMP=ISO, JSON=json string)
    # Returns:  {"value":"..."} or {"error":"..."}
    lib.J_SerializeValue.argtypes = [ctypes.c_char_p, ctypes.c_char_p]
    lib.J_SerializeValue.restype = ctypes.c_void_p


def _configure_d_symbols(lib: ctypes.CDLL) -> None:
    """Set argtypes and restype for all D_* exported functions."""
    # Import here to avoid circular imports at module load time
    from tdtp._structs_d import D_MaskConfig, D_Packet, D_FilterSpec

    # D_ReadFile(*char, *D_Packet) → c_int
    lib.D_ReadFile.argtypes = [ctypes.c_char_p, ctypes.POINTER(D_Packet)]
    lib.D_ReadFile.restype = ctypes.c_int

    # D_ParseBytes(*char, c_int, *D_Packet) → c_int  (in-memory counterpart
    # of D_ReadFile; parses a TDTP blob already in memory)
    lib.D_ParseBytes.argtypes = [
        ctypes.c_char_p, ctypes.c_int, ctypes.POINTER(D_Packet),
    ]
    lib.D_ParseBytes.restype = ctypes.c_int

    # D_WriteFile(*D_Packet, *char) → c_int
    lib.D_WriteFile.argtypes = [ctypes.POINTER(D_Packet), ctypes.c_char_p]
    lib.D_WriteFile.restype = ctypes.c_int

    # D_FreePacket(*D_Packet) → void
    lib.D_FreePacket.argtypes = [ctypes.POINTER(D_Packet)]
    lib.D_FreePacket.restype = None

    # D_FilterRows(*D_Packet, *D_FilterSpec, c_int, c_int, *D_Packet) → c_int
    lib.D_FilterRows.argtypes = [
        ctypes.POINTER(D_Packet),
        ctypes.POINTER(D_FilterSpec),
        ctypes.c_int,
        ctypes.c_int,
        ctypes.POINTER(D_Packet),
    ]
    lib.D_FilterRows.restype = ctypes.c_int

    # D_ApplyMask(*D_Packet, *D_MaskConfig, *D_Packet) → c_int
    lib.D_ApplyMask.argtypes = [
        ctypes.POINTER(D_Packet),
        ctypes.POINTER(D_MaskConfig),
        ctypes.POINTER(D_Packet),
    ]
    lib.D_ApplyMask.restype = ctypes.c_int

    # D_ApplyCompress(*D_Packet, c_int, *D_Packet) → c_int
    lib.D_ApplyCompress.argtypes = [
        ctypes.POINTER(D_Packet), ctypes.c_int, ctypes.POINTER(D_Packet),
    ]
    lib.D_ApplyCompress.restype = ctypes.c_int

    # D_ApplyDecompress(*D_Packet, *D_Packet) → c_int
    lib.D_ApplyDecompress.argtypes = [
        ctypes.POINTER(D_Packet), ctypes.POINTER(D_Packet),
    ]
    lib.D_ApplyDecompress.restype = ctypes.c_int

    # D_FreeMaskConfig(*D_MaskConfig) → void
    lib.D_FreeMaskConfig.argtypes = [ctypes.POINTER(D_MaskConfig)]
    lib.D_FreeMaskConfig.restype = None

    # --- Arrow columnar bridge ---------------------------------------------
    # D_ColumnFloat64(*D_Packet, c_int) → *double  (len = row_count; NaN = null)
    lib.D_ColumnFloat64.argtypes = [ctypes.POINTER(D_Packet), ctypes.c_int]
    lib.D_ColumnFloat64.restype = ctypes.POINTER(ctypes.c_double)

    # D_ColumnInt64(*D_Packet, c_int) → *longlong  (len = row_count; 0 = null)
    lib.D_ColumnInt64.argtypes = [ctypes.POINTER(D_Packet), ctypes.c_int]
    lib.D_ColumnInt64.restype = ctypes.POINTER(ctypes.c_longlong)

    # D_ColumnUTF8(*D_Packet, c_int, **int32 offsets, *int32 nbytes) → *char data
    lib.D_ColumnUTF8.argtypes = [
        ctypes.POINTER(D_Packet),
        ctypes.c_int,
        ctypes.POINTER(ctypes.POINTER(ctypes.c_int)),
        ctypes.POINTER(ctypes.c_int),
    ]
    lib.D_ColumnUTF8.restype = ctypes.c_void_p

    # D_FreeBuffer(void*) → void
    lib.D_FreeBuffer.argtypes = [ctypes.c_void_p]
    lib.D_FreeBuffer.restype = None


# ---------------------------------------------------------------------------
# Helpers used by both api_j and api_d
# ---------------------------------------------------------------------------

def free_string(ptr: int | None) -> None:
    """Release a *C.char returned by any J_* function.

    ptr is an integer address (c_void_p), not Python bytes.
    Passing 0 or None is safe (no-op).
    """
    if ptr:
        lib.J_FreeString(ptr)


def get_lib_version() -> str:
    """Return the native library version reported by J_GetVersion().

    This is the single source of truth (pkg/core/version.Version in Go),
    so the Python package version always matches the compiled core.
    """
    ptr = lib.J_GetVersion()
    try:
        return ctypes.string_at(ptr).decode("utf-8")
    finally:
        free_string(ptr)


# ---------------------------------------------------------------------------
# Module-level singleton — lazy load on first import
# ---------------------------------------------------------------------------

lib: ctypes.CDLL = _load(_find_library())
