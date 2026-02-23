"""
Shared pytest fixtures for J_* and D_* API tests.
"""
from __future__ import annotations

import json
import shutil
import tempfile
from pathlib import Path

import pytest

from tdtp import TDTPClientDirect, TDTPClientJSON

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------

REPO_ROOT = Path(__file__).parents[3]
SAMPLE_DIR = REPO_ROOT / "tests" / "testdata"   # existing repo test data


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
    """Path to a small sample .tdtp file used by both API tests.

    TODO: generate a minimal .tdtp fixture file in tests/testdata/
          if one doesn't exist yet (use packet.Generator in a Go helper or
          copy from examples/).
    """
    # TODO: locate or generate a representative sample file
    # TODO: raise pytest.skip("sample .tdtp not found") if missing
    raise NotImplementedError


@pytest.fixture(scope="session")
def sample_data_j(j_client: TDTPClientJSON, sample_tdtp_path: Path) -> dict:
    """Pre-parsed sample data via JSON API (parsed once per session)."""
    return j_client.J_read(str(sample_tdtp_path))


@pytest.fixture()
def tmp_dir(tmp_path: Path) -> Path:
    """Temporary directory cleaned up after each test."""
    return tmp_path
