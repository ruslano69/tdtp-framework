#!/usr/bin/env python3
"""
TDTP CLI Integration Tests — Apache Kafka

Covers what makes Kafka different from the other brokers: the broker refuses a
produce request by message.max.bytes, and it measures the *compressed record
batch as a whole*, not one record. Both halves of that sentence have bitten this
project, so both are tested here rather than assumed.

    K1  export → Kafka, messages actually land in the topic
    K2  roundtrip: export → import back → row-for-row match with the source
    K3  spool path (packet_kb) keeps every message under the broker limit
    K4  a batch over the limit is reported before the send, not after
    K5  a rejection names each packet and its size, not just a count
    K6  packet_kb holds whatever the data compresses to — the same settings
        deliver both ordinary and high-entropy tables

K4-K6 lower the broker's message.max.bytes to the Kafka default for the duration
of the run and restore it afterwards. The override is dynamic — it shadows the
static value from the container's environment and is removed in teardown, so the
broker is left exactly as found. A broker configured generously (the dev compose
sets 10 MB, and the running container may differ from it again) cannot reproduce
any of this, which is precisely how the problem stayed invisible.

Topics are created per run with a unique suffix and deleted in teardown: a topic
left behind by an interrupted run would otherwise be consumed from at a stale
offset and the results would be quietly wrong.


ЧТО ИМЕННО СТЕРЕГУТ K3 И K6
    Пайплайн с output.type kafka уходит в streaming-экспорт
    (processor.go: isBrokerStreaming), и тот долгое время не смотрел ни на
    packet_kb, ни на spool_dir, ни на mem_limit_mb — ноль обращений к ним.
    KafkaSpoolExporter при этом работал, но добраться до него из пайплайна
    можно было только задав output.fallback, который переключает на batch по
    совершенно другой причине. Защита от превышения существовала не на том
    пути, которым идут данные: части выходили дефолтные (~1.9 МБ), и
    несжимаемая таблица отвергалась, тогда как сжимаемая проскакивала через
    Snappy — один и тот же экспорт удавался или нет в зависимости от формы
    данных.

    Теперь ExportStream берёт размер части из packet_kb. K3 проверяет, что
    дробление действительно происходит, K6 — что исход перестал зависеть от
    сжимаемости. Если они снова покраснеют, это вернулось.

Prerequisites:
    - Kafka reachable at KAFKA_BROKER (docker-compose.dev.yml brings one up)
    - docker CLI on PATH, container KAFKA_CONTAINER running
      (needed for kafka-topics / kafka-configs; K4-K6 skip without it)
    - tdtpcli built:  go build -o /tmp/tdtpcli ./cmd/tdtpcli/

Usage:
    python tests/cli/test_kafka.py           # all groups
    python tests/cli/test_kafka.py K4        # single group
    TDTPCLI_BIN=/path/to/tdtpcli python tests/cli/test_kafka.py

Environment overrides:
    TDTPCLI_BIN       path to tdtpcli binary   (default: <tmp>/tdtpcli)
    KAFKA_BROKER      bootstrap server         (default: localhost:9092)
    KAFKA_CONTAINER   docker container name    (default: tdtp-kafka)
"""

import os
import random
import re
import shutil
import socket
import sqlite3
import string
import subprocess
import sys
import tempfile
import time
import uuid
from pathlib import Path

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

# ─── Configuration ────────────────────────────────────────────────────────────
# tempfile.gettempdir() rather than a hardcoded /tmp: tdtpcli is a Go binary and
# receives whatever path string this script hands it, with no POSIX translation
# of its own. Same reasoning as test_encryption.py.
_TMP    = Path(tempfile.gettempdir())
ROOT    = Path(__file__).resolve().parent.parent.parent
TDTPCLI = os.environ.get("TDTPCLI_BIN", str(_TMP / "tdtpcli"))

KAFKA_BROKER    = os.environ.get("KAFKA_BROKER", "localhost:9092")
KAFKA_CONTAINER = os.environ.get("KAFKA_CONTAINER", "tdtp-kafka")

TEST_DB = str(_TMP / "tdtp_kafka_test.db")
CFG     = str(_TMP / "tdtp_kafka_test.yaml")
OUTDIR  = _TMP / "tdtp_kafka_test_out"

# Unique per run: a topic surviving an interrupted run would be consumed from a
# stale offset and the roundtrip would compare against yesterday's data.
RUN_ID = uuid.uuid4().hex[:8]

# Kafka's own default, and what a managed broker (Confluent Cloud, Event Hubs,
# MSK) gives you without admin access.
KAFKA_DEFAULT_MAX_BYTES = 1048576

ROWS_PLAIN  = 20000   # ordinary, highly compressible
ROWS_RANDOM = 20000   # 200 random chars per row — resists compression

GREEN  = "\033[32m"
RED    = "\033[31m"
YELLOW = "\033[33m"
BOLD   = "\033[1m"
RESET  = "\033[0m"

results: list = []
_created_topics: list = []
_limit_lowered = False


# ─── Helpers ──────────────────────────────────────────────────────────────────

def record(tid: str, passed: bool, elapsed: float, msg: str = ""):
    results.append((tid, passed, elapsed, msg))
    status = f"{GREEN}PASS{RESET}" if passed else f"{RED}FAIL{RESET}"
    detail = f"  ({msg})" if msg and not passed else ""
    print(f"  [{status}] {tid:<54} {elapsed:.2f}s{detail}")


def skip(tid: str, why: str):
    results.append((tid, True, 0.0, f"skipped: {why}"))
    print(f"  [{YELLOW}SKIP{RESET}] {tid:<54} {why}")


def run(*args, timeout=120) -> subprocess.CompletedProcess:
    return subprocess.run([TDTPCLI, *args], capture_output=True, text=True, timeout=timeout)


def topic(name: str) -> str:
    return f"tdtp-test-{name}-{RUN_ID}"


def docker_available() -> bool:
    if shutil.which("docker") is None:
        return False
    p = subprocess.run(["docker", "exec", KAFKA_CONTAINER, "true"],
                       capture_output=True, text=True)
    return p.returncode == 0


def kafka_reachable() -> bool:
    host, _, port = KAFKA_BROKER.rpartition(":")
    try:
        with socket.create_connection((host or "localhost", int(port)), timeout=3):
            return True
    except OSError:
        return False


def kadmin(*args, timeout=60) -> subprocess.CompletedProcess:
    """Run a Kafka admin tool inside the broker container."""
    return subprocess.run(
        ["docker", "exec", KAFKA_CONTAINER, *args,
         "--bootstrap-server", "localhost:9092"],
        capture_output=True, text=True, timeout=timeout)


def create_topic(name: str) -> bool:
    p = kadmin("kafka-topics", "--create", "--topic", name,
               "--partitions", "1", "--replication-factor", "1")
    ok = p.returncode == 0 or "already exists" in (p.stdout + p.stderr)
    if ok:
        _created_topics.append(name)
    return ok


def topic_message_count(name: str) -> int:
    """Highest offset in partition 0 — how many messages the topic holds."""
    p = subprocess.run(
        ["docker", "exec", KAFKA_CONTAINER, "kafka-run-class",
         "kafka.tools.GetOffsetShell", "--bootstrap-server", "localhost:9092",
         "--topic", name],
        capture_output=True, text=True, timeout=60)
    total = 0
    for line in p.stdout.splitlines():
        parts = line.strip().split(":")
        if len(parts) == 3 and parts[2].isdigit():
            total += int(parts[2])
    return total


def set_broker_limit(value: int) -> bool:
    p = kadmin("kafka-configs", "--entity-type", "brokers", "--entity-name", "1",
               "--alter", "--add-config", f"message.max.bytes={value}")
    return p.returncode == 0


def reset_broker_limit() -> bool:
    """Remove the dynamic override; the static value from the container env
    takes over again, so the broker is left exactly as it was found."""
    p = kadmin("kafka-configs", "--entity-type", "brokers", "--entity-name", "1",
               "--alter", "--delete-config", "message.max.bytes")
    return p.returncode == 0


# ─── Fixtures ─────────────────────────────────────────────────────────────────

def setup_db():
    if os.path.exists(TEST_DB):
        os.remove(TEST_DB)
    conn = sqlite3.connect(TEST_DB)
    c = conn.cursor()

    # Ordinary table data: repetitive, compresses about threefold.
    c.execute("CREATE TABLE plain (id INTEGER PRIMARY KEY, name TEXT, city TEXT, amount REAL)")
    cities = ["Moscow", "SPb", "Kazan", "Sochi"]
    c.executemany("INSERT INTO plain VALUES (?,?,?,?)",
                  [(i, f"employee-{i}", cities[i % 4], i * 1.5) for i in range(ROWS_PLAIN)])

    # High-entropy payload: does not compress, so the broker sees close to the
    # uncompressed size. This is what UUID/base64/ciphertext columns look like.
    rnd = random.Random(42)
    alphabet = string.ascii_letters + string.digits
    c.execute("CREATE TABLE noisy (id INTEGER PRIMARY KEY, payload TEXT)")
    c.executemany("INSERT INTO noisy VALUES (?,?)",
                  [(i, "".join(rnd.choices(alphabet, k=200))) for i in range(ROWS_RANDOM)])

    conn.commit()
    conn.close()

    OUTDIR.mkdir(parents=True, exist_ok=True)


def write_cfg(topic_name: str):
    Path(CFG).write_text(
        "database:\n"
        "  type: sqlite\n"
        f"  dsn: {TEST_DB}\n"
        "broker:\n"
        "  type: kafka\n"
        f"  brokers: [\"{KAFKA_BROKER}\"]\n"
        f"  queue: {topic_name}\n"
        f"  consumer_group: tdtp-test-{RUN_ID}\n",
        encoding="utf-8")


def write_pipeline(path: str, table: str, topic_name: str, extra: str = ""):
    Path(path).write_text(
        f'name: "kafka-test-{table}"\n'
        'version: "1.0"\n'
        "sources:\n"
        f"  - name: {table}\n"
        "    type: sqlite\n"
        f"    dsn: {TEST_DB}\n"
        f'    query: "SELECT * FROM {table}"\n'
        "workspace:\n"
        "  type: sqlite\n"
        '  mode: ":memory:"\n'
        "transform:\n"
        "  result_table: result\n"
        f'  sql: "SELECT * FROM {table}"\n'
        "output:\n"
        "  type: kafka\n"
        "  kafka:\n"
        f'    brokers: ["{KAFKA_BROKER}"]\n'
        f'    topic: "{topic_name}"\n'
        + extra,
        encoding="utf-8")


# ─── K1: export lands in the topic ────────────────────────────────────────────

def test_K1_export():
    print(f"\n{BOLD}=== K1 export → Kafka ==={RESET}")

    name = topic("k1")
    if not create_topic(name):
        skip("K1.1 --export-broker delivers messages", "could not create topic")
        return
    write_cfg(name)

    t = time.monotonic()
    p = run("--config", CFG, "--export-broker", "plain")
    count = topic_message_count(name) if p.returncode == 0 else -1
    record("K1.1 --export-broker → messages present in topic",
           p.returncode == 0 and count > 0,
           time.monotonic() - t, f"rc={p.returncode} messages={count} err={p.stderr.strip()[:120]}")


# ─── K2: roundtrip through the broker ─────────────────────────────────────────

def test_K2_roundtrip():
    print(f"\n{BOLD}=== K2 export → import roundtrip ==={RESET}")

    name = topic("k2")
    if not create_topic(name):
        skip("K2.1 roundtrip", "could not create topic")
        return
    write_cfg(name)

    t = time.monotonic()
    p = run("--config", CFG, "--export-broker", "plain")
    record("K2.1 export for roundtrip", p.returncode == 0,
           time.monotonic() - t, f"rc={p.returncode} err={p.stderr.strip()[:120]}")
    if p.returncode != 0:
        return

    t = time.monotonic()
    p = run("--config", CFG, "--import-broker", "--table", "plain_from_kafka",
            "--strategy", "replace")
    imported = -1
    if p.returncode == 0:
        conn = sqlite3.connect(TEST_DB)
        try:
            imported = conn.execute("SELECT COUNT(*) FROM plain_from_kafka").fetchone()[0]
        except sqlite3.OperationalError:
            imported = -1
        finally:
            conn.close()
    record("K2.2 --import-broker restores every row",
           p.returncode == 0 and imported == ROWS_PLAIN,
           time.monotonic() - t, f"rc={p.returncode} rows={imported} want={ROWS_PLAIN}")

    # Content, not just the count: a roundtrip that loses column values while
    # keeping the row count would pass the check above.
    t = time.monotonic()
    conn = sqlite3.connect(TEST_DB)
    try:
        src = conn.execute("SELECT name, city, amount FROM plain WHERE id = 7").fetchone()
        dst = conn.execute("SELECT name, city, amount FROM plain_from_kafka WHERE id = 7").fetchone()
    except sqlite3.OperationalError:
        src, dst = None, None
    finally:
        conn.close()
    record("K2.3 row content survives the roundtrip",
           src is not None and dst is not None and tuple(map(str, src)) == tuple(map(str, dst)),
           time.monotonic() - t, f"src={src} dst={dst}")


# ─── K3: the spool path bounds message size ───────────────────────────────────

def test_K3_spool():
    print(f"\n{BOLD}=== K3 spool path (packet_kb) ==={RESET}")

    if not docker_available():
        skip("K3.1 spool keeps messages under the limit", "docker/container unavailable")
        return

    name = topic("k3")
    if not create_topic(name):
        skip("K3.1 spool keeps messages under the limit", "could not create topic")
        return

    pipeline = str(_TMP / f"tdtp_kafka_spool_{RUN_ID}.yaml")
    write_pipeline(pipeline, "noisy", name,
                   "    packet_kb: 200\n    batch_send: 1\n")

    t = time.monotonic()
    p = run("--pipeline", pipeline)
    count = topic_message_count(name)
    # packet_kb 200 over ~4 MB of rows has to produce many messages; one or two
    # would mean the setting was ignored, which is how this path failed before.
    record("K3.1 packet_kb splits the export into many small messages",
           p.returncode == 0 and count >= 10,
           time.monotonic() - t, f"rc={p.returncode} messages={count}")


# ─── K4: warning before the send ──────────────────────────────────────────────

def test_K4_warning():
    print(f"\n{BOLD}=== K4 oversize warning ==={RESET}")

    if not _limit_lowered:
        skip("K4.1 batch over the limit warns before sending", "broker limit not lowered")
        return

    name = topic("k4")
    if not create_topic(name):
        skip("K4.1 batch over the limit warns before sending", "could not create topic")
        return

    pipeline = str(_TMP / f"tdtp_kafka_warn_{RUN_ID}.yaml")
    # packet_kb deliberately far above the broker's limit. Note the unit:
    # packet_kb feeds SetMaxMessageSize, which budgets in UTF-16 units -- about
    # twice the byte count -- so 8000 asks for roughly 4 MB of actual XML
    # against a 1 MB limit. 2000 was not enough: it lands near 1 MB and slips
    # through.
    # Asking for a size that fits no longer produces a warning — that is what
    # the streaming fix achieved — so provoking one now means asking for a size
    # that genuinely does not.
    write_pipeline(pipeline, "noisy", name,
                   "    packet_kb: 8000\n    batch_send: 1\n")

    t = time.monotonic()
    p = run("--pipeline", pipeline)
    out = p.stdout + p.stderr
    # The warning is now about a single packet that cannot fit, not about a
    # batch: an oversized batch is split automatically, so there is nothing
    # left to warn about there. What splitting cannot fix is one part being
    # larger than the broker accepts.
    warned = "WARNING" in out and "a single Kafka message is" in out
    record("K4.1 oversized batch is reported before the send",
           warned, time.monotonic() - t, f"warned={warned}")

    record("K4.2 the warning names the recommended broker setting",
           warned and "4194304" in out,
           0.0, "recommended message.max.bytes missing from the warning")


# ─── K5: the rejection is classified ──────────────────────────────────────────

def test_K5_rejection():
    print(f"\n{BOLD}=== K5 rejection detail ==={RESET}")

    if not _limit_lowered:
        skip("K5.1 rejection reports each packet", "broker limit not lowered")
        return

    name = topic("k5")
    if not create_topic(name):
        skip("K5.1 rejection reports each packet", "could not create topic")
        return

    pipeline = str(_TMP / f"tdtp_kafka_reject_{RUN_ID}.yaml")
    # Same reasoning as K4: ask for parts the broker cannot accept.
    write_pipeline(pipeline, "noisy", name,
                   "    packet_kb: 8000\n    batch_send: 1\n")

    t = time.monotonic()
    p = run("--pipeline", pipeline)
    out = p.stdout + p.stderr

    # The old message was "streaming export completed with N errors" and said
    # nothing about size — the count was all that survived.
    named_cause = "message size over the broker limit" in out
    record("K5.1 rejection names the cause, not just a count",
           named_cause, time.monotonic() - t, f"out={out.strip()[-160:]}")

    # Breakdown, not a fixed count: the streaming path sends one message per
    # call, so "packet 1/1" is all there is to list there, while the spool path
    # batches and lists all ten. What has to hold on both is that each packet
    # is named with its size.
    per_packet = re.search(r"packet \d+/\d+: \d+ bytes", out) is not None
    record("K5.2 rejection lists each packet in the batch with its size",
           per_packet, 0.0, "per-packet breakdown missing")

    remedy = "batch_send" in out and "packet_kb" in out
    record("K5.3 rejection says which knob to turn",
           remedy, 0.0, "no remedy in the message")


# ─── K6: compression decides the outcome ──────────────────────────────────────

def test_K6_compressibility():
    print(f"\n{BOLD}=== K6 packet_kb holds for both data shapes ==={RESET}")

    if not _limit_lowered:
        skip("K6.1 sized parts fit whatever the data compresses to", "broker limit not lowered")
        return

    plain_topic, noisy_topic = topic("k6a"), topic("k6b")
    if not (create_topic(plain_topic) and create_topic(noisy_topic)):
        skip("K6.1 sized parts fit whatever the data compresses to", "could not create topics")
        return

    # Well under the 1 MB limit, so the outcome must not depend on how well the
    # data compresses. Before streaming honoured packet_kb this setting was
    # ignored here, parts came out at the ~1.9 MB default, and the high-entropy
    # table was rejected while the compressible one squeaked through Snappy —
    # the same export passing or failing on the shape of the data alone.
    settings = "    packet_kb: 200\n    batch_send: 1\n"

    pl = str(_TMP / f"tdtp_kafka_k6a_{RUN_ID}.yaml")
    write_pipeline(pl, "plain", plain_topic, settings)
    t = time.monotonic()
    p_plain = run("--pipeline", pl)
    plain_count = topic_message_count(plain_topic)

    pn = str(_TMP / f"tdtp_kafka_k6b_{RUN_ID}.yaml")
    write_pipeline(pn, "noisy", noisy_topic, settings)
    p_noisy = run("--pipeline", pn)
    noisy_count = topic_message_count(noisy_topic)

    record("K6.1 compressible data delivers",
           p_plain.returncode == 0 and plain_count > 0,
           time.monotonic() - t, f"rc={p_plain.returncode} messages={plain_count}")

    record("K6.2 high-entropy data delivers on the same settings",
           p_noisy.returncode == 0 and noisy_count > 0,
           0.0, f"rc={p_noisy.returncode} messages={noisy_count}")

    # The incompressible table has to become more messages than the
    # compressible one at the same packet_kb, since each part carries the same
    # uncompressed budget but far more bytes on the wire.
    record("K6.3 high-entropy data splits into more parts than compressible data",
           noisy_count > plain_count > 0,
           0.0, f"plain={plain_count} noisy={noisy_count}")


# ─── Runner ───────────────────────────────────────────────────────────────────

GROUPS = [
    ("K1", test_K1_export),
    ("K2", test_K2_roundtrip),
    ("K3", test_K3_spool),
    ("K4", test_K4_warning),
    ("K5", test_K5_rejection),
    ("K6", test_K6_compressibility),
]


def preflight() -> bool:
    print(f"{BOLD}TDTP CLI — Kafka integration tests{RESET}")
    print(f"  tdtpcli : {TDTPCLI}")
    print(f"  broker  : {KAFKA_BROKER}")
    print(f"  run id  : {RUN_ID}")

    if not os.path.exists(TDTPCLI) and shutil.which(TDTPCLI) is None:
        print(f"  {RED}tdtpcli not found — build it: go build -o {TDTPCLI} ./cmd/tdtpcli/{RESET}")
        return False

    if not kafka_reachable():
        print(f"  {RED}{KAFKA_BROKER} is not reachable — start it: "
              f"docker compose -f docker-compose.dev.yml up -d kafka{RESET}")
        return False

    if docker_available():
        print(f"  docker  : {GREEN}{KAFKA_CONTAINER} reachable{RESET}")
    else:
        print(f"  docker  : {YELLOW}unavailable — K3-K6 will skip{RESET}")

    return True


def teardown():
    global _limit_lowered

    if _limit_lowered:
        if reset_broker_limit():
            print(f"  {GREEN}broker message.max.bytes override removed{RESET}")
        else:
            print(f"  {RED}could not remove the message.max.bytes override — "
                  f"check the broker by hand{RESET}")
        _limit_lowered = False

    for name in _created_topics:
        kadmin("kafka-topics", "--delete", "--topic", name)
    if _created_topics:
        print(f"  {GREEN}deleted {len(_created_topics)} test topic(s){RESET}")


def main():
    requested = set(sys.argv[1:]) if len(sys.argv) > 1 else None

    if not preflight():
        sys.exit(1)

    print(f"\n{BOLD}Preparing fixtures...{RESET}")
    setup_db()
    print(f"  {GREEN}{ROWS_PLAIN} compressible + {ROWS_RANDOM} high-entropy rows{RESET}")

    global _limit_lowered
    needs_limit = requested is None or requested & {"K4", "K5", "K6"}
    if needs_limit and docker_available():
        if set_broker_limit(KAFKA_DEFAULT_MAX_BYTES):
            _limit_lowered = True
            print(f"  {GREEN}broker message.max.bytes lowered to "
                  f"{KAFKA_DEFAULT_MAX_BYTES} for this run{RESET}")
            time.sleep(2)  # the dynamic config needs a moment to propagate
        else:
            print(f"  {YELLOW}could not lower message.max.bytes — K4-K6 will skip{RESET}")

    try:
        for name, fn in GROUPS:
            if requested is None or name in requested:
                fn()
    finally:
        print(f"\n{BOLD}Tearing down...{RESET}")
        teardown()

    print(f"\n{BOLD}{'=' * 58}{RESET}")
    print(f"{BOLD}SUMMARY{RESET}")
    passed = sum(1 for _, ok, _, _ in results if ok)
    total = len(results)
    color = GREEN if passed == total else RED
    print(f"  {color}PASSED: {passed} / {total}{RESET}")
    duration = sum(e for _, _, e, _ in results)
    failed = [(tid, msg) for tid, ok, _, msg in results if not ok]
    if failed:
        print(f"  {RED}DURATION: {duration:.1f}s{RESET}")
        print("\n  Failed tests:")
        for tid, msg in failed:
            print(f"    {RED}✗ {tid}{RESET}  {msg}")
    else:
        print(f"  DURATION: {duration:.1f}s")
    print(f"{'=' * 58}")
    sys.exit(0 if passed == total else 1)


if __name__ == "__main__":
    main()
