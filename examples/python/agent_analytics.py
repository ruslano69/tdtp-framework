#!/usr/bin/env python3
"""
Agent recipe: in-process analytics over a TDTP packet — no database, no temp files.

Read a TDTP file straight into an Apache Arrow table via the columnar bridge,
then query it with DuckDB (SQL), polars, and numpy. This is the Phase 3 payoff:
the agent holds the data in hand and computes at C speed.

Run:  python3 examples/python/agent_analytics.py
Requires:  make build-lib-full  +  pip install tdtp[arrow] duckdb
"""
from __future__ import annotations

import random
import tempfile
from pathlib import Path

from tdtp import Tdtp


def build_orders(n: int = 50_000) -> dict:
    random.seed(7)
    regions = ["North", "South", "East", "West", "Central"]
    return {
        "schema": {"Fields": [
            {"Name": "OrderID", "Type": "INTEGER", "Key": True},
            {"Name": "Region", "Type": "TEXT"},
            {"Name": "Total", "Type": "REAL"},
            {"Name": "Items", "Type": "INTEGER"},
        ]},
        "header": {"type": "reference", "table_name": "orders",
                   "message_id": "orders-1", "timestamp": "2026-05-30T00:00:00Z"},
        "data": [
            [str(i), random.choice(regions),
             f"{random.random() * 1000:.2f}", str(random.randint(1, 20))]
            for i in range(n)
        ],
    }


def main() -> None:
    db = Tdtp()
    work = Path(tempfile.mkdtemp())
    f = work / "orders.tdtp.xml"
    db.write(build_orders(), str(f))

    # Optionally verify integrity here (db.verify) before trusting the data.
    # One columnar read → Arrow table; everything below computes on it directly.
    tbl = db.read_arrow(str(f))
    print(f"loaded {tbl.num_rows} orders into Arrow ({tbl.num_columns} columns)")

    # 1. SQL with DuckDB — no database server, queries the Arrow table in place.
    try:
        import duckdb
        revenue = duckdb.query(
            "SELECT Region, count(*) orders, round(sum(Total), 2) revenue "
            "FROM tbl GROUP BY Region ORDER BY revenue DESC"
        ).to_df()
        print("\nrevenue by region (DuckDB SQL on Arrow):")
        print(revenue.to_string(index=False))
    except ImportError:
        print("(install duckdb to run the SQL step)")

    # 2. numpy — zero-copy column, vectorized stats.
    import numpy as np
    totals = tbl.column("Total").to_numpy()
    print(f"\nnumpy: total revenue = {totals.sum():,.2f}, "
          f"mean order = {totals.mean():.2f}, p95 = {np.percentile(totals, 95):.2f}")

    # 3. polars — Arrow-native, lazy filter + aggregate.
    try:
        import polars as pl
        big = (pl.from_arrow(tbl)
               .filter(pl.col("Items") >= 10)
               .group_by("Region")
               .agg(pl.col("Total").mean().alias("avg_total"))
               .sort("Region"))
        print("\npolars: avg total of large orders (>=10 items) by region:")
        print(big)
    except ImportError:
        print("(install polars to run the dataframe step)")


if __name__ == "__main__":
    main()
