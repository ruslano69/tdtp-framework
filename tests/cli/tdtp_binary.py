"""Shared preflight: refuse to test a binary that is not this tree's code.

Every suite under tests/cli defaults TDTPCLI to /tmp/tdtpcli and used to do no
more than print its --version banner. Both ways that goes wrong have happened:

  * A build eleven minor versions behind sat there for months. Runs were green
    or red for reasons that had nothing to do with the working tree — one of
    them reported a NULL-date crash fixed long before.
  * A binary of the *right* version, built an hour before the feature under
    test, passed the tests asserting that feature by not implementing it. A
    version comparison cannot see this; only the timestamp can.

A green run against the wrong binary is worse than a red one: it is a false
statement about the code. Hence both checks, and hence a hard exit rather than
a warning.

Usage from a suite's preflight():

    from tdtp_binary import check_binary
    check_binary(TDTPCLI)
"""

import os
import re
import subprocess
import sys
import time
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
VERSION_GO = REPO_ROOT / "pkg" / "core" / "version" / "version.go"

RED = "\033[31m"
RESET = "\033[0m"


def repo_version() -> str:
    """Version constant declared in pkg/core/version/version.go, '' if unreadable."""
    try:
        text = VERSION_GO.read_text(encoding="utf-8")
    except OSError:
        return ""
    m = re.search(r'Version\s*=\s*"([^"]+)"', text)
    return m.group(1) if m else ""


def newest_source_mtime() -> tuple:
    """(mtime, repo-relative path) of the newest .go file under cmd/ and pkg/."""
    newest, where = 0.0, ""
    for sub in ("cmd", "pkg"):
        root = REPO_ROOT / sub
        if not root.is_dir():
            continue
        for path in root.rglob("*.go"):
            try:
                mt = path.stat().st_mtime
            except OSError:
                continue
            if mt > newest:
                newest, where = mt, str(path.relative_to(REPO_ROOT))
    return newest, where


def build_hint(binary: str) -> str:
    return (f"  GOPROXY=https://goproxy.io GONOSUMDB='*' "
            f"go build -tags nokafka -o {binary} ./cmd/tdtpcli/")


def check_binary(binary: str, quiet: bool = False) -> str:
    """Verify the binary exists, matches this tree's version, and is not stale.

    Exits the process with a build hint on any failure. Returns the version
    string on success.

    Note on paths: Git Bash resolves /tmp to the Windows temp directory while
    Python resolves it against the current drive. Building from a shell and
    running the suite from Python can therefore touch two different files, and
    the timestamp check is what surfaces that.
    """
    if not os.path.exists(binary):
        print(f"{RED}ERROR: tdtpcli binary not found at {binary}{RESET}")
        print("Build first:")
        print(build_hint(binary))
        sys.exit(1)

    proc = subprocess.run([binary, "--version"], capture_output=True, text=True)
    m = re.search(r"version\s+(\S+)", proc.stdout)
    binary_ver = m.group(1) if m else ""
    if not quiet:
        print(f"tdtpcli: {binary_ver or proc.stdout.strip()}  ({binary})")

    expected = repo_version()
    if expected and binary_ver and binary_ver != expected:
        print(f"{RED}ERROR: binary is version {binary_ver}, this tree declares "
              f"{expected}{RESET}")
        print(f"  {VERSION_GO.relative_to(REPO_ROOT)} is the source of truth.")
        print("Rebuild:")
        print(build_hint(binary))
        print(f"  or point the suite elsewhere: TDTPCLI_BIN=/path/to/tdtpcli")
        sys.exit(1)

    src_mtime, src_path = newest_source_mtime()
    try:
        bin_mtime = os.path.getmtime(binary)
    except OSError:
        bin_mtime = 0.0
    if src_mtime and bin_mtime and src_mtime > bin_mtime:
        age = (src_mtime - bin_mtime) / 60.0
        print(f"{RED}ERROR: binary is older than the source it should contain{RESET}")
        print(f"  binary: {time.strftime('%Y-%m-%d %H:%M', time.localtime(bin_mtime))}")
        print(f"  source: {time.strftime('%Y-%m-%d %H:%M', time.localtime(src_mtime))} "
              f"({src_path}, {age:.0f} min newer)")
        print("Rebuild:")
        print(build_hint(binary))
        sys.exit(1)

    return binary_ver


def license_env(base: dict = None) -> dict:
    """Env with TDTP_LICENSE pointing at the repo's tdtp.lic, if there is one.

    tdtpcli resolves its licence from TDTP_LICENSE, else ./tdtp.lic relative to
    the working directory. The suites run from tests/cli, so ./tdtp.lic is not
    found and the binary falls back to the community tier — where --enc is
    refused outright with `feature "enc" is not licensed`.

    That is worth naming precisely, because the failure it produces looks like
    something else entirely: the encrypted file is written correctly and
    decrypts, and only the exit code says no. In test_encryption.py it read as
    a Mercury or HMAC problem for as long as nobody checked stderr.

    Suites that exercise no licensed feature are unaffected, which is why this
    surfaced only in the encryption suite.
    """
    env = dict(base if base is not None else os.environ)
    if "TDTP_LICENSE" not in env:
        lic = REPO_ROOT / "tdtp.lic"
        if lic.is_file():
            env["TDTP_LICENSE"] = str(lic)
    return env
