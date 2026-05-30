"""
Comparative benchmarks: J_* (JSON boundary) vs D_* (Direct/ctypes boundary).

Run with:
    pytest tests/test_bench.py -v --benchmark-sort=name

Each benchmark group exercises the same logical operation through both APIs
so pytest-benchmark can display a side-by-side comparison.

Naming convention:
    bench_<operation>_j  — JSON API
    bench_<operation>_d  — Direct API
"""
from __future__ import annotations

import pytest

from tdtp import TDTPClientDirect, TDTPClientJSON
from tests.conftest import SAMPLE_FILE


# ---------------------------------------------------------------------------
# Shared setup
# ---------------------------------------------------------------------------

@pytest.fixture(scope="module")
def j() -> TDTPClientJSON:
    return TDTPClientJSON()


@pytest.fixture(scope="module")
def d() -> TDTPClientDirect:
    return TDTPClientDirect()


@pytest.fixture(scope="module")
def sample_path() -> str:
    return str(SAMPLE_FILE)


# Pre-parsed data for operations that start from in-memory state.
@pytest.fixture(scope="module")
def j_data(j, sample_path):
    return j.J_read(sample_path)


# Pre-loaded D_Packet kept alive for the entire module (freed at teardown).
# Allows filter_only / mask_only benchmarks to skip file I/O.
@pytest.fixture(scope="module")
def d_handle(d, sample_path):
    h = d.D_read(sample_path)
    yield h
    h.free()


# ---------------------------------------------------------------------------
# Read benchmark
# ---------------------------------------------------------------------------

@pytest.mark.benchmark(group="read")
def test_bench_read_j(benchmark, j, sample_path):
    """Read .tdtp → JSON dict (J_ReadFile)."""
    benchmark(j.J_read, sample_path)


@pytest.mark.benchmark(group="read")
def test_bench_read_d(benchmark, d, sample_path):
    """Read .tdtp → D_Packet (D_ReadFile), then free."""
    def _read_and_free():
        h = d.D_read(sample_path)
        h.free()

    benchmark(_read_and_free)


# ---------------------------------------------------------------------------
# Read + extract rows benchmark
# ---------------------------------------------------------------------------

@pytest.mark.benchmark(group="read_rows")
def test_bench_read_rows_j(benchmark, j, sample_path):
    """Read file and return rows as Python list (JSON API)."""
    def _op():
        data = j.J_read(sample_path)
        return data["data"]

    benchmark(_op)


@pytest.mark.benchmark(group="read_rows")
def test_bench_read_rows_d(benchmark, d, sample_path):
    """Read file and extract rows as Python list (Direct API)."""
    def _op():
        with d.D_read_ctx(sample_path) as h:
            return h.get_rows()

    benchmark(_op)


# ---------------------------------------------------------------------------
# Filter benchmark (Balance > 1000)
# ---------------------------------------------------------------------------

@pytest.mark.benchmark(group="filter")
def test_bench_filter_j(benchmark, j, j_data):
    """Filter rows via JSON API (J_FilterRows)."""
    benchmark(j.J_filter, j_data, "Balance > 1000")


@pytest.mark.benchmark(group="filter")
def test_bench_filter_d(benchmark, d, sample_path):
    """Filter rows via Direct API (D_FilterRows)."""
    spec = [{"field": "Balance", "op": "gt", "value": "1000"}]

    def _op():
        with d.D_read_ctx(sample_path) as src:
            with d.D_filter(src, spec) as out:
                return out.get_rows()

    benchmark(_op)


# ---------------------------------------------------------------------------
# Mask benchmark (mask Email field)
# ---------------------------------------------------------------------------

@pytest.mark.benchmark(group="mask")
def test_bench_mask_j(benchmark, j, j_data):
    """Mask Email via JSON processor chain (J_ApplyProcessor)."""
    benchmark(j.J_apply_processor, j_data, "field_masker", fields={"Email": "stars"})


@pytest.mark.benchmark(group="mask")
def test_bench_mask_d(benchmark, d, sample_path):
    """Mask Email via Direct API (D_ApplyMask)."""
    def _op():
        with d.D_read_ctx(sample_path) as src:
            with d.D_apply_mask(src, ["Email"], visible_chars=0) as out:
                return out.get_rows()

    benchmark(_op)


# ---------------------------------------------------------------------------
# Compress / decompress roundtrip
# ---------------------------------------------------------------------------

@pytest.mark.benchmark(group="compress")
def test_bench_compress_roundtrip_j(benchmark, j, j_data):
    """Compress + decompress via JSON API."""
    def _op():
        compressed = j.J_apply_processor(j_data, "compress", level=3)
        return j.J_apply_processor(compressed, "decompress")

    benchmark(_op)


@pytest.mark.benchmark(group="compress")
def test_bench_compress_roundtrip_d(benchmark, d, sample_path):
    """Compress + decompress via Direct API."""
    def _op():
        with d.D_read_ctx(sample_path) as src:
            with d.D_compress(src, level=3) as c:
                with d.D_decompress(c) as dec:
                    return dec.get_rows()

    benchmark(_op)


# ---------------------------------------------------------------------------
# Row extraction overhead (in-memory, no I/O)
# ---------------------------------------------------------------------------

@pytest.mark.benchmark(group="extract_rows")
def test_bench_extract_rows_j(benchmark, j_data):
    """Access rows from an already-parsed JSON dict."""
    benchmark(lambda: j_data["data"])


@pytest.mark.benchmark(group="extract_rows")
def test_bench_extract_rows_d(benchmark, d, sample_path):
    """Call get_rows() on an already-opened D_Packet."""
    h = d.D_read(sample_path)
    try:
        benchmark(h.get_rows)
    finally:
        h.free()


# ---------------------------------------------------------------------------
# FAIR: filter_only — both APIs start from already-loaded in-memory data,
# no file I/O inside the benchmark loop.
#
# J_*: j_data is a Python dict already in memory → J_FilterRows
#        (JSON serialize dict → call Go → JSON deserialize result)
# D_*: d_handle is a D_Packet already in C memory → D_FilterRows
#        (pass C pointer → Go filters in-place → return C pointer)
# ---------------------------------------------------------------------------

@pytest.mark.benchmark(group="filter_only")
def test_bench_filter_only_j(benchmark, j, j_data):
    """Filter in-memory J dict, no file I/O  (J_FilterRows)."""
    benchmark(j.J_filter, j_data, "Balance > 1000")


@pytest.mark.benchmark(group="filter_only")
def test_bench_filter_only_d(benchmark, d, d_handle):
    """Filter in-memory D_Packet, no file I/O  (D_FilterRows)."""
    spec = [{"field": "Balance", "op": "gt", "value": "1000"}]

    def _op():
        with d.D_filter(d_handle, spec) as out:
            return out.get_rows()

    benchmark(_op)


# ---------------------------------------------------------------------------
# FAIR: mask_only — same idea, no I/O inside loop.
# ---------------------------------------------------------------------------

@pytest.mark.benchmark(group="mask_only")
def test_bench_mask_only_j(benchmark, j, j_data):
    """Mask Email in-memory J dict, no file I/O  (J_ApplyProcessor)."""
    benchmark(j.J_apply_processor, j_data, "field_masker", fields={"Email": "stars"})


@pytest.mark.benchmark(group="mask_only")
def test_bench_mask_only_d(benchmark, d, d_handle):
    """Mask Email in-memory D_Packet, no file I/O  (D_ApplyMask)."""
    def _op():
        with d.D_apply_mask(d_handle, ["Email"], visible_chars=0) as out:
            return out.get_rows()

    benchmark(_op)


# ---------------------------------------------------------------------------
# FAIR: pipeline — read → filter → get_rows as one atomic operation.
# Both APIs do exactly the same work end-to-end.
# ---------------------------------------------------------------------------

@pytest.mark.benchmark(group="pipeline")
def test_bench_pipeline_j(benchmark, j, sample_path):
    """Read + filter + return rows  (J API, end-to-end)."""
    def _op():
        data = j.J_read(sample_path)
        filtered = j.J_filter(data, "Balance > 1000")
        return filtered["data"]

    benchmark(_op)


@pytest.mark.benchmark(group="pipeline")
def test_bench_pipeline_d(benchmark, d, sample_path):
    """Read + filter + return rows  (D API, end-to-end)."""
    spec = [{"field": "Balance", "op": "gt", "value": "1000"}]

    def _op():
        with d.D_read_ctx(sample_path) as src:
            with d.D_filter(src, spec) as out:
                return out.get_rows()

    benchmark(_op)


# ---------------------------------------------------------------------------
# Arrow write path benchmark: itertuples vs columnar (Phase 3)
#
# Build a 10 000-row table with INT + FLOAT + TEXT columns, then write it to
# a temporary TDTP file using two approaches:
#   - "row"  path: pandas itertuples + _serialize per cell (J_WriteFile)
#   - "col"  path: numpy vectorized column extraction + J_WriteColumnar
# ---------------------------------------------------------------------------

@pytest.fixture(scope="module")
def large_arrow_table():
    """10 000-row Arrow table: INTEGER + REAL + TEXT."""
    pa = pytest.importorskip("pyarrow")
    np = pytest.importorskip("numpy")
    rng = np.random.default_rng(42)
    n = 10_000
    ids    = pa.array(rng.integers(1, 100_000, n), type=pa.int64())
    scores = pa.array(rng.uniform(0.0, 1000.0, n), type=pa.float64())
    tags   = pa.array([f"tag_{i % 100}" for i in range(n)], type=pa.string())
    return pa.table({"ID": ids, "Score": scores, "Tag": tags})


@pytest.fixture(scope="module")
def large_pandas_df(large_arrow_table):
    """Same data as a pandas DataFrame (for the itertuples baseline)."""
    return large_arrow_table.to_pandas()


@pytest.mark.benchmark(group="write_10k")
def test_bench_write_row_itertuples(benchmark, j, tmp_path, large_pandas_df):
    """Write 10 000 rows — row-major itertuples path (pandas_to_data → J_WriteFile)."""
    pytest.importorskip("pyarrow")
    from tdtp.pandas_ext import pandas_to_data

    out = str(tmp_path / "bench_row.tdtp.xml")

    def _op():
        data = pandas_to_data(large_pandas_df, table_name="bench")
        j.J_write(data, out)

    benchmark(_op)


@pytest.mark.benchmark(group="write_10k")
def test_bench_write_col_arrow(benchmark, tmp_path, large_arrow_table):
    """Write 10 000 rows — columnar Arrow path (write_arrow / J_WriteColumnar)."""
    pytest.importorskip("pyarrow")
    from tdtp.arrow_ext import write_arrow

    out = str(tmp_path / "bench_col.tdtp.xml")

    def _op():
        write_arrow(large_arrow_table, out, table_name="bench")

    benchmark(_op)


@pytest.mark.benchmark(group="write_10k")
def test_bench_write_from_arrow_to_dict(benchmark, large_arrow_table):
    """Convert 10 000-row Arrow table → data dict only (no file I/O)."""
    pytest.importorskip("pyarrow")
    from tdtp.arrow_ext import arrow_to_data

    benchmark(arrow_to_data, large_arrow_table, table_name="bench")
