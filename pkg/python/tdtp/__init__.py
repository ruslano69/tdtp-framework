"""
TDTP Framework - Python bindings
Express data integration library
"""

from .core import DataFrame, read_tdtp
from .version import __version__

# Optional pandas integration
try:
    from .pandas_adapter import from_pandas
    __all__ = [
        'DataFrame',
        'read_tdtp',
        'from_pandas',
        '__version__',
    ]
except ImportError:
    # pandas not installed
    __all__ = [
        'DataFrame',
        'read_tdtp',
        '__version__',
    ]
