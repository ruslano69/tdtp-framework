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
