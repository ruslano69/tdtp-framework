"""
Pure Python TDTP XML parser (fallback when Go library not available)
"""

import xml.etree.ElementTree as ET
import base64
from typing import Dict, List, Any


def parse_tdtp_xml(path: str) -> Dict[str, Any]:
    """
    Parse TDTP XML file using pure Python (no Go library required)

    This is a fallback parser that doesn't require Go shared library.
    Note: Does NOT support zstd decompression - use Go library for compressed files.

    Args:
        path: Path to TDTP XML file

    Returns:
        Dict with 'schema', 'data', 'header' keys

    Raises:
        RuntimeError: If file uses compression (requires Go library)
    """
    tree = ET.parse(path)
    root = tree.getroot()

    # Parse header
    header_elem = root.find('Header')
    header = {}
    if header_elem is not None:
        for child in header_elem:
            header[child.tag] = child.text

    # Parse schema
    schema_elem = root.find('Schema')
    fields = []
    if schema_elem is not None:
        for field_elem in schema_elem.findall('Field'):
            field = {
                'name': field_elem.get('name', ''),
                'type': field_elem.get('type', 'string'),
            }
            # Optional attributes
            if field_elem.get('length'):
                field['length'] = int(field_elem.get('length'))
            if field_elem.get('precision'):
                field['precision'] = int(field_elem.get('precision'))
            if field_elem.get('scale'):
                field['scale'] = int(field_elem.get('scale'))
            if field_elem.get('key'):
                field['key'] = field_elem.get('key') == 'true'
            if field_elem.get('readonly'):
                field['readonly'] = field_elem.get('readonly') == 'true'

            fields.append(field)

    schema = {'Fields': fields}

    # Parse data
    data_elem = root.find('Data')
    compression = data_elem.get('compression') if data_elem is not None else None

    if compression == 'zstd':
        raise RuntimeError(
            "This TDTP file uses zstd compression. "
            "Pure Python parser doesn't support compression. "
            "Please build and use Go shared library: "
            "cd pkg/python && make build"
        )

    # Parse rows (uncompressed)
    rows = []
    if data_elem is not None:
        for row_elem in data_elem.findall('R'):
            row_text = row_elem.text or ''
            # Split by pipe (|) - TDTP row delimiter
            values = _parse_row_values(row_text)
            rows.append(values)

    return {
        'schema': schema,
        'data': rows,
        'header': header,
        'error': None
    }


def _parse_row_values(row_text: str) -> List[str]:
    """
    Parse row values from TDTP format

    TDTP uses | as delimiter with escaping:
    - || = literal |
    - |\n = literal newline
    - etc.

    Args:
        row_text: Raw row text from XML

    Returns:
        List of field values
    """
    if not row_text:
        return []

    values = []
    current = []
    i = 0

    while i < len(row_text):
        char = row_text[i]

        if char == '|':
            # Check if escaped
            if i + 1 < len(row_text):
                next_char = row_text[i + 1]
                if next_char == '|':
                    # Escaped pipe
                    current.append('|')
                    i += 2
                    continue
                elif next_char == 'n':
                    # Escaped newline
                    current.append('\n')
                    i += 2
                    continue
                elif next_char == 'r':
                    # Escaped carriage return
                    current.append('\r')
                    i += 2
                    continue
                elif next_char == 't':
                    # Escaped tab
                    current.append('\t')
                    i += 2
                    continue

            # Not escaped - field delimiter
            values.append(''.join(current))
            current = []
            i += 1
        else:
            current.append(char)
            i += 1

    # Last field
    values.append(''.join(current))

    return values


def can_use_go_library() -> bool:
    """Check if Go shared library is available"""
    try:
        from pathlib import Path
        lib_dir = Path(__file__).parent
        lib_names = ['libtdtp.so', 'libtdtp.dylib', 'libtdtp.dll']
        return any((lib_dir / name).exists() for name in lib_names)
    except:
        return False
