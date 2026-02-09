"""
Core TDTP DataFrame implementation
"""

import ctypes
import json
import os
from pathlib import Path
from typing import List, Dict, Any, Optional


class DataFrame:
    """
    TDTP DataFrame - lightweight data container

    Internal representation: list of rows (each row is a list of values)
    Similar to pandas DataFrame but simpler and focused on data transfer
    """

    def __init__(self, data: List[List[Any]], schema: Dict[str, Any], header: Optional[Dict[str, Any]] = None):
        """
        Initialize DataFrame

        Args:
            data: List of rows (each row is list of values)
            schema: TDTP schema dict with 'Fields' key
            header: Optional TDTP header dict
        """
        self._data = data
        self._schema = schema
        self._header = header or {}

        # Extract column names from schema
        self._columns = [field['name'] for field in schema.get('Fields', [])]

    @property
    def data(self) -> List[List[Any]]:
        """Raw data as list of rows"""
        return self._data

    @property
    def schema(self) -> Dict[str, Any]:
        """TDTP schema"""
        return self._schema

    @property
    def header(self) -> Dict[str, Any]:
        """TDTP header (metadata)"""
        return self._header

    @property
    def columns(self) -> List[str]:
        """Column names"""
        return self._columns

    @property
    def shape(self) -> tuple:
        """Shape of DataFrame (rows, columns)"""
        return (len(self._data), len(self._columns))

    def __len__(self) -> int:
        """Number of rows"""
        return len(self._data)

    def __getitem__(self, key):
        """
        Access data by column name or row index

        df['column_name'] -> list of values
        df[0] -> dict with first row
        """
        if isinstance(key, str):
            # Get column by name
            if key not in self._columns:
                raise KeyError(f"Column '{key}' not found")
            col_idx = self._columns.index(key)
            return [row[col_idx] if col_idx < len(row) else None for row in self._data]

        elif isinstance(key, int):
            # Get row by index
            if key < 0 or key >= len(self._data):
                raise IndexError(f"Row index {key} out of range")
            return dict(zip(self._columns, self._data[key]))

        else:
            raise TypeError(f"Invalid key type: {type(key)}")

    def to_dict(self, orient: str = 'records') -> Any:
        """
        Convert DataFrame to dict

        Args:
            orient: 'records' (list of dicts) or 'list' (dict of lists)

        Returns:
            Data as dict in specified format
        """
        if orient == 'records':
            # [{'col1': val1, 'col2': val2}, ...]
            return [dict(zip(self._columns, row)) for row in self._data]

        elif orient == 'list':
            # {'col1': [val1, val2, ...], 'col2': [...], ...}
            return {col: self[col] for col in self._columns}

        else:
            raise ValueError(f"Unknown orient: {orient}")

    def head(self, n: int = 5) -> 'DataFrame':
        """Return first n rows"""
        return DataFrame(self._data[:n], self._schema, self._header)

    def tail(self, n: int = 5) -> 'DataFrame':
        """Return last n rows"""
        return DataFrame(self._data[-n:], self._schema, self._header)

    def to_pandas(self):
        """
        Convert to pandas DataFrame

        Returns:
            pandas.DataFrame

        Raises:
            ImportError: If pandas is not installed

        Example:
            >>> import tdtp
            >>> df_tdtp = tdtp.read_tdtp('file.tdtp.xml')
            >>> df_pandas = df_tdtp.to_pandas()
            >>> print(df_pandas.describe())
        """
        from .pandas_adapter import to_pandas
        return to_pandas(self)

    def info(self) -> str:
        """Return DataFrame info as string"""
        lines = [
            f"TDTP DataFrame",
            f"Shape: {self.shape[0]} rows × {self.shape[1]} columns",
            f"",
            f"Columns:",
        ]

        for field in self._schema.get('Fields', []):
            field_type = field.get('type', 'unknown')
            field_name = field.get('name', 'unknown')
            lines.append(f"  {field_name:20s} {field_type}")

        if self._header:
            lines.append(f"")
            lines.append(f"Metadata:")
            lines.append(f"  Table: {self._header.get('TableName', 'unknown')}")
            lines.append(f"  Records: {self._header.get('RecordsInPart', len(self._data))}")
            if self._header.get('TotalParts', 1) > 1:
                lines.append(f"  Part: {self._header.get('PartNumber', 1)} of {self._header.get('TotalParts', 1)}")

        return '\n'.join(lines)

    def __repr__(self) -> str:
        return f"DataFrame({self.shape[0]} rows × {self.shape[1]} columns)"

    def __str__(self) -> str:
        """Pretty print first few rows"""
        lines = [repr(self), ""]

        # Header
        lines.append("  ".join(f"{col:15s}" for col in self._columns[:5]))
        lines.append("-" * 80)

        # First 5 rows
        for row in self._data[:5]:
            values = [str(v)[:15] for v in row[:5]]
            lines.append("  ".join(f"{v:15s}" for v in values))

        if len(self._data) > 5:
            lines.append(f"... ({len(self._data) - 5} more rows)")

        return '\n'.join(lines)


def read_tdtp(path: str) -> DataFrame:
    """
    Read TDTP XML file into DataFrame

    Automatically handles:
    - zstd compression + base64 decoding (requires Go library)
    - Schema parsing
    - Data parsing
    - Pagination (if multiple parts)

    Uses Go shared library for performance and compression support.
    Falls back to pure Python parser for uncompressed files if Go library not available.

    Args:
        path: Path to TDTP XML file

    Returns:
        DataFrame with data and schema

    Raises:
        RuntimeError: If file cannot be read or parsed
        FileNotFoundError: If file doesn't exist
    """
    if not os.path.exists(path):
        raise FileNotFoundError(f"File not found: {path}")

    # Try to use Go shared library (fast, supports compression)
    try:
        lib = _load_library()

        # Call Go function
        result_json = lib.ReadTDTP(path.encode('utf-8'))
        result_str = result_json.decode('utf-8')

        # Parse JSON result
        result = json.loads(result_str)

        # Check for errors
        if 'error' in result and result['error']:
            raise RuntimeError(f"Failed to read TDTP: {result['error']}")

        # Create DataFrame
        return DataFrame(
            data=result['data'],
            schema=result['schema'],
            header=result.get('header')
        )

    except RuntimeError as e:
        # Go library not available - try pure Python parser (fallback)
        if "shared library not found" in str(e).lower():
            from .xml_parser import parse_tdtp_xml

            result = parse_tdtp_xml(path)

            if result.get('error'):
                raise RuntimeError(result['error'])

            return DataFrame(
                data=result['data'],
                schema=result['schema'],
                header=result.get('header')
            )
        else:
            # Other error - re-raise
            raise


def _load_library():
    """Load shared library (cached)"""
    global _lib_cache

    if '_lib_cache' not in globals():
        # Find library
        lib_dir = Path(__file__).parent

        # Try different extensions based on platform
        lib_names = ['libtdtp.so', 'libtdtp.dylib', 'libtdtp.dll']

        lib_path = None
        for name in lib_names:
            candidate = lib_dir / name
            if candidate.exists():
                lib_path = candidate
                break

        if not lib_path:
            raise RuntimeError(
                f"TDTP shared library not found. "
                f"Expected one of {lib_names} in {lib_dir}. "
                f"Please build the library first: "
                f"cd pkg/python/libtdtp && go build -buildmode=c-shared -o ../tdtp/libtdtp.so main.go"
            )

        # Load library
        lib = ctypes.CDLL(str(lib_path))

        # Define functions
        lib.ReadTDTP.argtypes = [ctypes.c_char_p]
        lib.ReadTDTP.restype = ctypes.c_char_p

        lib.GetVersion.argtypes = []
        lib.GetVersion.restype = ctypes.c_char_p

        _lib_cache = lib

    return _lib_cache


# Global library cache
_lib_cache = None
