"""
Smoke tests for the agent recipe examples — guard against bit-rot.

Each example is run as a subprocess with PYTHONPATH pointing at the bindings
package and TDTP_LIB_PATH at the compiled .so, and must exit 0.
"""
from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

import pytest

_BINDINGS = Path(__file__).resolve().parents[1]          # bindings/python
_EXAMPLES = _BINDINGS.parents[1] / "examples" / "python"  # repo/examples/python
_LIB = _BINDINGS / "tdtp" / "libtdtp.so"

EXAMPLES = ["agent_pipeline.py", "agent_diff_merge.py"]


@pytest.mark.parametrize("script", EXAMPLES)
def test_example_runs(script: str) -> None:
    path = _EXAMPLES / script
    if not path.exists():
        pytest.skip(f"{script} not present")
    env = dict(os.environ, PYTHONPATH=str(_BINDINGS), TDTP_LIB_PATH=str(_LIB))
    result = subprocess.run(
        [sys.executable, str(path)],
        capture_output=True, text=True, env=env, timeout=60,
    )
    assert result.returncode == 0, f"{script} failed:\n{result.stderr}"
    assert result.stdout.strip()  # produced some output
