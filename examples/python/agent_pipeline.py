#!/usr/bin/env python3
"""
Agent recipe: a trust-and-transform pipeline using the Tdtp facade.

Demonstrates the verbs an agent reaches for most, end to end:

    build → stamp (sign) → verify (trust) → filter → sort → export → test

Run:  python3 examples/python/agent_pipeline.py
Requires:  make build-lib-full   (so the .so is on TDTP_LIB_PATH or in tdtp/)
"""
from __future__ import annotations

import tempfile
from pathlib import Path

from tdtp import Tdtp


def build_dataset() -> dict:
    """Construct a TDTP dataset dict by hand (no DB needed).

    The shape matches what Tdtp.read() returns: schema + header + data rows.
    """
    return {
        "schema": {"Fields": [
            {"Name": "ID", "Type": "INTEGER", "Key": True},
            {"Name": "Name", "Type": "TEXT"},
            {"Name": "City", "Type": "TEXT"},
            {"Name": "Balance", "Type": "INTEGER"},
        ]},
        "header": {
            "type": "reference",
            "table_name": "customers",
            "message_id": "demo-0001",
            "timestamp": "2026-05-30T00:00:00Z",
        },
        "data": [
            ["1", "Ann",   "Moscow", "3200"],
            ["2", "Boris", "Omsk",   "150"],
            ["3", "Cara",  "Moscow", "9100"],
            ["4", "Dmitri","Kazan",  "780"],
            ["5", "Elena", "Moscow", "5400"],
        ],
    }


def main() -> None:
    db = Tdtp()
    print(f"tdtp {db.version}")

    work = Path(tempfile.mkdtemp())
    data = build_dataset()

    # 1. Sign: write a v1.4 integrity packet and capture its fingerprint.
    signed = work / "customers.tdtp.xml"
    stamp = db.stamp(data, str(signed))
    print(f"signed → {stamp['packet_xxh3']}")

    # 2. Trust: a consumer verifies the packet was not tampered before using it.
    verdict = db.verify(str(signed))
    if not (verdict["has_integrity"] and verdict["ok"]):
        raise SystemExit(f"refusing tampered packet: {verdict.get('detail')}")
    print("verified OK")

    # 3. Transform: read it back, keep the high-value Moscow customers, sort them.
    loaded = db.read(str(signed))
    rich = db.filter(loaded, "City = 'Moscow' AND Balance > 1000")
    ranked = db.sort(rich, [{"field": "Balance", "direction": "desc"}])
    bal = [f["Name"] for f in ranked["schema"]["Fields"]].index("Balance")
    print("top Moscow customers:",
          [(r[1], r[bal]) for r in ranked["data"]])

    # 4. Export the result (compressed + checksummed) and dry-run its integrity.
    out = work / "moscow_rich.tdtp.xml"
    res = db.export(ranked, str(out), compress=True, checksum=True)
    report = db.test(res["files"][0])
    print(f"exported {report['total_rows']} rows, integrity ok={report['ok']}, "
          f"compression={report['parts'][0]['compression']}")


if __name__ == "__main__":
    main()
