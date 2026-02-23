"""
Native library loader.

Locates libtdtp.so (or .dll / .dylib) relative to this package,
configures ctypes argtypes / restype for every exported symbol,
and exposes a single module-level `lib` instance.

Usage (internal):
    from tdtp._loader import lib, free_string
"""
from __future__ import annotations

import ctypes
import os
import platform
import sys
from pathlib import Path

from tdtp.exceptions import TDTPLibraryError

# ---------------------------------------------------------------------------
# Locate shared library
# ---------------------------------------------------------------------------

def _find_library() -> Path:
    """Return the path to the platform-specific libtdtp shared library.

    Search order:
      1. TDTP_LIB_PATH environment variable (absolute path to .so/.dll/.dylib)
      2. Directory of this file  (installed wheel bundles the .so alongside)
      3. Repository root build output  (development: ``make build-lib``)
    """
    # TODO: implement search order as described above
    # TODO: raise TDTPLibraryError if not found with helpful message
    raise TDTPLibraryError("TODO: library discovery not implemented")


def _load(lib_path: Path) -> ctypes.CDLL:
    """Load the shared library and configure all symbol signatures."""
    try:
        _lib = ctypes.CDLL(str(lib_path))
    except OSError as exc:
        raise TDTPLibraryError(f"Cannot load {lib_path}: {exc}") from exc

    _configure_j_symbols(_lib)
    _configure_d_symbols(_lib)
    return _lib


# ---------------------------------------------------------------------------
# Symbol configuration — JSON API (J_*)
# ---------------------------------------------------------------------------

def _configure_j_symbols(lib: ctypes.CDLL) -> None:
    """Set argtypes and restype for all J_* exported functions."""
    # TODO: J_GetVersion  () → c_char_p
    # TODO: J_FreeString  (c_char_p) → None
    # TODO: J_ReadFile    (c_char_p) → c_char_p
    # TODO: J_WriteFile   (c_char_p, c_char_p) → c_char_p
    # TODO: J_FilterRows  (c_char_p, c_char_p, c_int) → c_char_p
    # TODO: J_ApplyProcessor (c_char_p, c_char_p, c_char_p) → c_char_p
    # TODO: J_ApplyChain  (c_char_p, c_char_p) → c_char_p
    # TODO: J_Diff        (c_char_p, c_char_p) → c_char_p


# ---------------------------------------------------------------------------
# Symbol configuration — Direct API (D_*)
# ---------------------------------------------------------------------------

def _configure_d_symbols(lib: ctypes.CDLL) -> None:
    """Set argtypes and restype for all D_* exported functions.

    Requires _structs_d to be importable (circular-import safe because
    _structs_d does not import _loader).
    """
    # TODO: import D_Packet, D_FilterSpec, D_MaskConfig from ._structs_d
    # TODO: D_ReadFile     (c_char_p, POINTER(D_Packet)) → c_int
    # TODO: D_WriteFile    (POINTER(D_Packet), c_char_p) → c_int
    # TODO: D_FreePacket   (POINTER(D_Packet)) → None
    # TODO: D_FilterRows   (POINTER(D_Packet), POINTER(D_FilterSpec), c_int, c_int,
    #                        POINTER(D_Packet)) → c_int
    # TODO: D_ApplyMask    (POINTER(D_Packet), POINTER(D_MaskConfig),
    #                        POINTER(D_Packet)) → c_int
    # TODO: D_ApplyCompress   (POINTER(D_Packet), c_int, POINTER(D_Packet)) → c_int
    # TODO: D_ApplyDecompress (POINTER(D_Packet), POINTER(D_Packet)) → c_int
    # TODO: D_FreeMaskConfig  (POINTER(D_MaskConfig)) → None


# ---------------------------------------------------------------------------
# Helpers used by both api_j and api_d
# ---------------------------------------------------------------------------

def free_string(ptr: ctypes.c_char_p) -> None:
    """Release a *C.char returned by any J_* function."""
    # TODO: lib.J_FreeString(ptr)


# ---------------------------------------------------------------------------
# Module-level singleton
# ---------------------------------------------------------------------------

# Lazy-loaded on first import; raises TDTPLibraryError if .so not found.
lib: ctypes.CDLL = _load(_find_library())
