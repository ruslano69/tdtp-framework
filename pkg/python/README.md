# TDTP Framework - Python Bindings

Python library for reading TDTP (Table Data Transfer Protocol) files.

## Features

- 🚀 Fast: Uses Go shared library under the hood
- 📦 Zero dependencies (stdlib only)
- 🗜️ Automatic decompression (zstd + base64)
- 📄 Pagination support (multi-part files)
- 🐼 Pandas-like API

## Installation

### From source

```bash
# 1. Build Go shared library
cd pkg/python/libtdtp
go build -buildmode=c-shared -o ../tdtp/libtdtp.so main.go

# 2. Install Python package
cd ..
pip install -e .
```

### From PyPI (coming soon)

```bash
pip install tdtp-framework
```

## Quick Start

```python
import tdtp

# Read TDTP file (with automatic decompression)
df = tdtp.read_tdtp('orders.tdtp.xml')

# Basic info
print(df.shape)       # (1000, 5)
print(df.columns)     # ['id', 'customer', 'total', 'date', 'status']

# Access data
print(df[0])          # First row as dict
print(df['customer']) # Customer column as list

# Convert to dict
records = df.to_dict('records')
# [{'id': 1, 'customer': 'John', ...}, ...]

# Pretty print
print(df.head())      # First 5 rows
print(df.info())      # Schema info
```

## API Reference

### `read_tdtp(path: str) -> DataFrame`

Read TDTP XML file into DataFrame. Automatically handles:
- zstd compression + base64 decoding
- Schema parsing
- Data type detection
- Multi-part files (pagination)

**Parameters:**
- `path` (str): Path to TDTP XML file

**Returns:**
- `DataFrame`: TDTP DataFrame with data and schema

**Raises:**
- `FileNotFoundError`: If file doesn't exist
- `RuntimeError`: If file cannot be parsed

### `DataFrame`

Lightweight data container similar to pandas DataFrame.

**Properties:**
- `data` (List[List[Any]]): Raw data as list of rows
- `schema` (dict): TDTP schema with field definitions
- `header` (dict): TDTP header with metadata
- `columns` (List[str]): Column names
- `shape` (tuple): (rows, columns)

**Methods:**
- `__len__()`: Number of rows
- `__getitem__(key)`: Access by column name or row index
- `to_dict(orient='records')`: Convert to dict
- `head(n=5)`: First n rows
- `tail(n=5)`: Last n rows
- `info()`: Display schema and metadata

**Examples:**

```python
# Access column
customers = df['customer']  # List of values

# Access row
first_row = df[0]  # Dict: {'id': 1, 'customer': 'John', ...}

# Iterate rows
for i in range(len(df)):
    row = df[i]
    print(row['customer'], row['total'])

# Convert to list of dicts
records = df.to_dict('records')

# Convert to dict of lists
lists = df.to_dict('list')
# {'customer': ['John', 'Jane', ...], 'total': [100, 200, ...]}
```

## Use Cases

### 1. Quick Data Inspection

```python
import tdtp

# Read and inspect
df = tdtp.read_tdtp('export.tdtp.xml')
print(df.info())  # Show schema
print(df.head())  # Show first rows
```

### 2. Integration with pandas

```python
import tdtp
import pandas as pd

# TDTP → pandas
df_tdtp = tdtp.read_tdtp('data.tdtp.xml')
df_pandas = pd.DataFrame(df_tdtp.to_dict('records'))

# Now use pandas functionality
print(df_pandas.describe())
df_pandas.to_csv('output.csv')
```

### 3. Data Processing

```python
import tdtp

# Read TDTP
df = tdtp.read_tdtp('orders.tdtp.xml')

# Filter in Python
high_value = [
    row for row in df.to_dict('records')
    if row['total'] > 1000
]

# Process
for order in high_value:
    print(f"Order {order['id']}: ${order['total']}")
```

### 4. Multi-file Processing

```python
import tdtp
from pathlib import Path

# Process all TDTP files in directory
for file in Path('exports').glob('*.tdtp.xml'):
    df = tdtp.read_tdtp(str(file))
    print(f"{file.name}: {df.shape[0]} rows")
```

## Architecture

TDTP Python module uses Go shared library (CGO) for:
- XML parsing
- zstd decompression
- base64 decoding
- Data validation

This provides:
- ⚡ High performance (native Go code)
- 🔒 Reliability (reuses battle-tested Go implementation)
- 🪶 Lightweight (no Python dependencies)

## Building from Source

### Prerequisites

- Go 1.19+
- Python 3.7+
- GCC (for CGO)

### Build Steps

```bash
# 1. Clone repository
git clone https://github.com/ruslano69/tdtp-framework.git
cd tdtp-framework

# 2. Build Go shared library
cd pkg/python/libtdtp
go build -buildmode=c-shared -o ../tdtp/libtdtp.so main.go

# 3. Install Python package
cd ..
pip install -e .

# 4. Test
python -c "import tdtp; print(tdtp.__version__)"
```

### Cross-compilation

```bash
# Linux
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
  go build -buildmode=c-shared -o libtdtp.so main.go

# macOS
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 \
  go build -buildmode=c-shared -o libtdtp.dylib main.go

# Windows (requires mingw-w64)
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
  go build -buildmode=c-shared -o libtdtp.dll main.go
```

## Roadmap

**Phase 1 (Current):** ✅
- Read TDTP files
- DataFrame API
- Automatic decompression

**Phase 2 (Next):**
- Write TDTP files
- Pandas integration helpers
- Excel export

**Phase 3 (Future):**
- SQL adapters (SQLAlchemy)
- TDTQL filtering
- Incremental sync

## License

MIT License - see LICENSE file

## Links

- GitHub: https://github.com/ruslano69/tdtp-framework
- Issues: https://github.com/ruslano69/tdtp-framework/issues
- CLI tool: See main README for tdtpcli
