#!/usr/bin/env python3
"""
TDTP Python Example
Demonstrates basic usage of TDTP Python bindings
"""

import tdtp
import sys
from pathlib import Path


def main():
    print("TDTP Framework - Python Bindings Example")
    print("=" * 50)
    print()

    # Check if file provided
    if len(sys.argv) < 2:
        print("Usage: python example.py <path-to-tdtp-file>")
        print()
        print("Example:")
        print("  python example.py test_data.tdtp.xml")
        print()
        print("You can create test TDTP file using tdtpcli:")
        print("  tdtpcli --export users --output test_data.tdtp.xml")
        sys.exit(1)

    file_path = sys.argv[1]

    # Check file exists
    if not Path(file_path).exists():
        print(f"Error: File not found: {file_path}")
        sys.exit(1)

    print(f"Reading TDTP file: {file_path}")
    print()

    try:
        # Read TDTP file
        df = tdtp.read_tdtp(file_path)

        # Show info
        print(df.info())
        print()

        # Show shape
        print(f"Shape: {df.shape[0]} rows × {df.shape[1]} columns")
        print()

        # Show first 5 rows
        print("First 5 rows:")
        print("-" * 80)
        for i in range(min(5, len(df))):
            row = df[i]
            print(f"Row {i+1}:")
            for col in df.columns[:5]:  # Show first 5 columns
                value = row.get(col, 'N/A')
                print(f"  {col:20s} = {str(value)[:50]}")
            print()

        # Show column statistics
        print("Columns:")
        for col in df.columns:
            values = df[col]
            non_null = len([v for v in values if v is not None and v != ''])
            print(f"  {col:20s} - {non_null}/{len(values)} non-null values")

        print()

        # Convert to dict
        print("Converting to dict (records format)...")
        records = df.to_dict('records')
        print(f"✅ Converted {len(records)} records")
        print()

        # Sample record
        if records:
            print("Sample record (first row):")
            for key, value in list(records[0].items())[:5]:
                print(f"  {key:20s} = {str(value)[:50]}")

        print()
        print("✅ Success!")

    except Exception as e:
        print(f"❌ Error: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)


if __name__ == '__main__':
    main()
