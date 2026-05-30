"""
Tests for the high-level Tdtp facade — the recommended agent entry point.

The facade delegates to TDTPClientJSON, so these tests focus on the facade
surface (plain verbs, version, lazy direct client) rather than re-testing the
underlying J_* semantics covered in test_api_j.py.
"""
from __future__ import annotations

import re

import pytest

from tdtp import Tdtp, TDTPClientDirect, TDTPClientJSON

from conftest import SAMPLE_FIELD_NAMES, SAMPLE_TOTAL_ROWS


@pytest.fixture
def db() -> Tdtp:
    return Tdtp()


class TestFacadeBasics:
    def test_version_matches_json_client(self, db) -> None:
        assert db.version == TDTPClientJSON().J_get_version()
        assert re.match(r"\d+\.\d+\.\d+", db.version)

    def test_repr_includes_version(self, db) -> None:
        assert db.version in repr(db)

    def test_json_escape_hatch(self, db) -> None:
        assert isinstance(db.json, TDTPClientJSON)

    def test_direct_is_lazy(self, db) -> None:
        assert db._d is None          # not built until accessed
        assert isinstance(db.direct, TDTPClientDirect)
        assert db._d is db.direct     # cached on second access


class TestFacadeVerbs:
    def test_read(self, db, sample_tdtp_path) -> None:
        data = db.read(str(sample_tdtp_path))
        assert len(data["data"]) == SAMPLE_TOTAL_ROWS

    def test_filter(self, db, sample_tdtp_path) -> None:
        data = db.read(str(sample_tdtp_path))
        out = db.filter(data, "Balance > 1000")
        bal_idx = SAMPLE_FIELD_NAMES.index("Balance")
        assert all(int(r[bal_idx]) > 1000 for r in out["data"])

    def test_sort(self, db, sample_tdtp_path) -> None:
        data = db.read(str(sample_tdtp_path))
        out = db.sort(data, "Balance")
        idx = SAMPLE_FIELD_NAMES.index("Balance")
        bals = [int(r[idx]) for r in out["data"]]
        assert bals == sorted(bals)

    def test_merge(self, db, sample_tdtp_path) -> None:
        data = db.read(str(sample_tdtp_path))
        out = db.merge([data, data], strategy="append")
        assert out["stats"]["total_rows_out"] == 2 * SAMPLE_TOTAL_ROWS

    def test_inspect(self, db, sample_tdtp_path) -> None:
        assert db.inspect(str(sample_tdtp_path))["total_rows"] == SAMPLE_TOTAL_ROWS

    def test_test(self, db, sample_tdtp_path) -> None:
        assert db.test(str(sample_tdtp_path))["ok"] is True

    def test_write_roundtrip(self, db, sample_tdtp_path, tmp_path) -> None:
        data = db.read(str(sample_tdtp_path))
        out = tmp_path / "out.tdtp.xml"
        db.write(data, str(out))
        assert db.read(str(out))["data"] == data["data"]


class TestFacadeIntegrity:
    def test_stamp_then_verify(self, db, sample_tdtp_path, tmp_path) -> None:
        data = db.read(str(sample_tdtp_path))
        f = tmp_path / "signed.tdtp.xml"
        r = db.stamp(data, str(f))
        assert len(r["packet_xxh3"]) == 32
        v = db.verify(str(f))
        assert v["ok"] is True and v["has_integrity"] is True

    def test_verify_detects_tamper(self, db, sample_tdtp_path, tmp_path) -> None:
        data = db.read(str(sample_tdtp_path))
        f = tmp_path / "signed.tdtp.xml"
        db.stamp(data, str(f))
        raw = open(f).read()
        open(f, "w").write(raw.replace("Moscow", "Madrid", 1))
        assert db.verify(str(f))["ok"] is False
