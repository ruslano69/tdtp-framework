"""
Shared pytest fixtures for J_* and D_* API tests.
"""
from __future__ import annotations

from pathlib import Path

import pytest

from tdtp import TDTPClientDirect, TDTPClientJSON

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------

TESTS_DIR   = Path(__file__).parent
TESTDATA    = TESTS_DIR / "testdata"
SAMPLE_FILE = TESTDATA / "users.tdtp.xml"

# Known sample data properties (kept in sync with users.tdtp.xml)
SAMPLE_TOTAL_ROWS    = 8
SAMPLE_FIELD_NAMES   = ["ID", "Name", "Email", "City", "Balance", "IsActive", "CreatedAt"]
SAMPLE_KEY_FIELD     = "ID"
# rows with Balance > 1000
SAMPLE_BALANCE_GT_1000_COUNT = 5
# rows where City = 'Moscow'
SAMPLE_MOSCOW_COUNT  = 2


# ---------------------------------------------------------------------------
# Client fixtures
# ---------------------------------------------------------------------------

@pytest.fixture(scope="session")
def j_client() -> TDTPClientJSON:
    """Shared TDTPClientJSON instance (stateless, safe to reuse)."""
    return TDTPClientJSON()


@pytest.fixture(scope="session")
def d_client() -> TDTPClientDirect:
    """Shared TDTPClientDirect instance (stateless, safe to reuse)."""
    return TDTPClientDirect()


# ---------------------------------------------------------------------------
# Sample file fixtures
# ---------------------------------------------------------------------------

@pytest.fixture(scope="session")
def sample_tdtp_path() -> Path:
    """Path to the bundled sample .tdtp fixture."""
    if not SAMPLE_FILE.exists():
        pytest.skip(f"Sample fixture not found: {SAMPLE_FILE}")
    return SAMPLE_FILE


@pytest.fixture(scope="session")
def sample_data_j(j_client: TDTPClientJSON, sample_tdtp_path: Path) -> dict:
    """Pre-parsed sample data via JSON API (parsed once per session)."""
    return j_client.J_read(str(sample_tdtp_path))


@pytest.fixture()
def tmp_tdtp(tmp_path: Path) -> Path:
    """A temporary path for writing .tdtp output files."""
    return tmp_path / "out.tdtp.xml"
