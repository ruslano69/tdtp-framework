"""
TDTP Framework - Python bindings
Express data integration library
"""

from .core import DataFrame, read_tdtp
from .version import __version__

__all__ = [
    'DataFrame',
    'read_tdtp',
    '__version__',
]
