#!/usr/bin/env python3
"""
Agent recipe: reconcile two daily exports with diff + merge.

A common agent task: yesterday's and today's snapshots arrive as TDTP files;
report what changed, then produce a single deduplicated current view.

Run:  python3 examples/python/agent_diff_merge.py
Requires:  make build-lib-full
"""
from __future__ import annotations

import tempfile
from pathlib import Path

from tdtp import Tdtp

SCHEMA = {"Fields": [
    {"Name": "ID", "Type": "INTEGER", "Key": True},
    {"Name": "Status", "Type": "TEXT"},
]}


def snapshot(message_id: str, rows: list[list[str]]) -> dict:
    return {
        "schema": SCHEMA,
        "header": {"type": "reference", "table_name": "orders",
                   "message_id": message_id, "timestamp": "2026-05-30T00:00:00Z"},
        "data": rows,
    }


def main() -> None:
    db = Tdtp()

    monday = snapshot("mon", [["1", "new"], ["2", "paid"], ["3", "new"]])
    tuesday = snapshot("tue", [["2", "paid"], ["3", "shipped"], ["4", "new"]])

    # 1. Diff: what changed between the two snapshots?
    d = db.diff(monday, tuesday)
    print("added   :", d["added"])      # order 4 appeared
    print("removed :", d["removed"])    # order 1 disappeared
    print("modified:", [(m["key"], m["changes"]) for m in d["modified"]])  # 3: new→shipped
    print("stats   :", d["stats"])

    # 2. Merge: build the current view. right-priority means Tuesday wins on conflict.
    merged = db.merge([monday, tuesday], strategy="right", key_fields=["ID"])
    print("merged rows:", merged["data"])
    print("duplicates removed:", merged["stats"]["duplicates"])

    # 3. Persist the reconciled view as a signed packet for downstream consumers.
    work = Path(tempfile.mkdtemp())
    out = work / "orders_current.tdtp.xml"
    fp = db.stamp(merged, str(out))
    print(f"signed current view → {fp['packet_xxh3']}  ({out})")


if __name__ == "__main__":
    main()
