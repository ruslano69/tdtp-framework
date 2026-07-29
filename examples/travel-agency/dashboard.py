#!/usr/bin/env python3
"""
Travel Agency dashboard -- watch the replication run, in a browser.

    python dashboard.py            # then open http://localhost:8100

Read-only. It polls four sources and joins them; it never publishes, consumes,
writes to a database or moves a checkpoint. Kill it mid-flight and nothing
notices -- which is the whole reason it is allowed to be Python in an example
whose data path deliberately has none. coordinator.py was Python that *moved
data*; this is a window.

    orchestrator :8099   /jobs, /schedules  -- the export side, run by run
    RabbitMQ     :15672  /api/queues        -- depth, consumers, publish/deliver rates
    state/audit_*.db                        -- the import side, one row per message
    state/*.json                            -- the watermarks

Each source is polled independently and its failure is reported in place, so a
stopped orchestrator does not blank the queue view.

The entity list is not written here. It is derived from workflows/*.yaml (which
table, which config) and configs/*.yaml (which queue) -- the same files the
workflow itself uses. A dashboard with its own copy of the topology is a
dashboard that will eventually describe a system that no longer exists.

Standard library only: no pika, no redis, no requests, no PyYAML.
"""

import argparse
import base64
import glob
import json
import os
import re
import sqlite3
import sys
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# Same reason as activity.py: a redirected stdout on Windows is not UTF-8, and
# the startup banner has box-drawing characters in it.
for _stream in (sys.stdout, sys.stderr):
    if hasattr(_stream, 'reconfigure'):
        _stream.reconfigure(encoding='utf-8', errors='replace')

TRAVEL_DIR = os.path.dirname(os.path.abspath(__file__))
STATE_DIR = os.path.join(TRAVEL_DIR, "state")

# ─── Topology discovery ──────────────────────────────────────────────────────


def discover_entities():
    """Build [{id, table, queue, kind}] from the workflow and config files.

    Regex rather than a YAML parser to keep the standard-library-only promise.
    The shapes read here are the two lines every step already has -- the
    command's table argument and its --config -- so this stays correct as long
    as the workflow does.
    """
    entities = []
    for wf in sorted(glob.glob(os.path.join(TRAVEL_DIR, "workflows", "*.yaml"))):
        with open(wf, encoding="utf-8") as fh:
            text = fh.read()
        for block in re.split(r"^\s*-\s+id:\s*", text, flags=re.M)[1:]:
            step_id = block.split("\n", 1)[0].strip()
            table = re.search(r"--(?:sync-incremental|export-broker)\s+(\S+)", block)
            cfg = re.search(r"--config\s+(\S+)", block)
            if not table or not cfg:
                continue
            queue = ""
            cfg_path = os.path.join(TRAVEL_DIR, cfg.group(1))
            if os.path.exists(cfg_path):
                with open(cfg_path, encoding="utf-8") as fh:
                    m = re.search(r"^\s*queue:\s*(\S+)", fh.read(), re.M)
                if m:
                    queue = m.group(1)
            entities.append({
                "id": step_id,
                "table": table.group(1),
                "queue": queue,
                # A full reload has no watermark, and saying so is more useful
                # than showing an empty cell that looks like a fault.
                "kind": "incremental" if "--sync-incremental" in block else "full",
            })
    return entities


# ─── Sources ─────────────────────────────────────────────────────────────────


def fetch_json(url, auth=None, timeout=3.0):
    req = urllib.request.Request(url)
    if auth:
        token = base64.b64encode(("%s:%s" % auth).encode()).decode()
        req.add_header("Authorization", "Basic " + token)
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read().decode("utf-8"))


ROWS_RE = re.compile(r"Records synced: (\d+)")


def orchestrator_state(base):
    """Recent jobs and schedules. Rows come from the job log, which is the only
    place the count exists -- the orchestrator stores tdtpcli's output verbatim
    and does not parse it."""
    out = {"jobs": [], "schedules": [], "error": None}
    try:
        jobs = fetch_json(base + "/jobs?limit=15") or []
        for j in jobs:
            log = j.get("log") or ""
            out["jobs"].append({
                "scenario": j.get("scenario"),
                "status": j.get("status"),
                "finished_at": j.get("finished_at"),
                "rows": sum(int(n) for n in ROWS_RE.findall(log)),
                "idle": log.count("No new changes to sync"),
                "failed_steps": re.findall(r"\[steps\] .*?(\w+): FAILED", log),
            })
        out["schedules"] = [{
            "id": s.get("ID"),
            "cron": s.get("CronExpr"),
            "last_status": s.get("LastStatus"),
            "last_run": s.get("LastRunAt"),
            "next_run": s.get("NextRunAt"),
        } for s in (fetch_json(base + "/schedules") or [])]
    except (urllib.error.URLError, OSError, ValueError) as exc:
        out["error"] = str(exc)
    return out


def rabbit_state(base, user, password):
    """Per-queue depth, consumer count and rates.

    consumers is the field worth watching: a queue with messages and no
    consumer is a stalled import, and it looks identical to a healthy idle
    queue if you only track depth.
    """
    cols = ("name,messages,consumers,"
            "message_stats.publish_details.rate,"
            "message_stats.deliver_get_details.rate")
    out = {"queues": {}, "error": None}
    try:
        for q in fetch_json("%s/api/queues/%%2F?columns=%s" % (base, cols),
                            auth=(user, password)) or []:
            stats = q.get("message_stats") or {}
            out["queues"][q["name"]] = {
                "messages": q.get("messages", 0),
                "consumers": q.get("consumers", 0),
                "publish_rate": (stats.get("publish_details") or {}).get("rate", 0.0),
                "deliver_rate": (stats.get("deliver_get_details") or {}).get("rate", 0.0),
            }
    except (urllib.error.URLError, OSError, ValueError, KeyError) as exc:
        out["error"] = str(exc)
    return out


def audit_state():
    """Imported messages per queue, from every audit database in state/.

    Opened read-only through a file: URI so this can never take SQLite's write
    lock away from a listener that is mid-upsert.
    """
    out = {"by_queue": {}, "failures": [], "error": None}
    dbs = sorted(glob.glob(os.path.join(STATE_DIR, "audit_*.db")))
    if not dbs:
        out["error"] = "no audit database yet (audit: is configured per node in configs/)"
        return out
    for path in dbs:
        try:
            uri = "file:%s?mode=ro" % path.replace("\\", "/")
            conn = sqlite3.connect(uri, uri=True, timeout=2.0)
            try:
                rows = conn.execute(
                    "SELECT resource, COUNT(*), COALESCE(SUM(records_affected),0), MAX(timestamp) "
                    "FROM audit_log GROUP BY resource")
                for resource, msgs, rec, last in rows:
                    queue = (resource or "").replace("broker://", "")
                    agg = out["by_queue"].setdefault(
                        queue, {"messages": 0, "records": 0, "last": ""})
                    agg["messages"] += msgs
                    agg["records"] += rec
                    agg["last"] = max(agg["last"], last or "")
                for resource, status, msg, ts in conn.execute(
                        "SELECT resource, status, error_message, timestamp FROM audit_log "
                        "WHERE status <> 'success' ORDER BY timestamp DESC LIMIT 5"):
                    out["failures"].append({
                        "queue": (resource or "").replace("broker://", ""),
                        "status": status, "error": msg, "at": ts,
                    })
            finally:
                conn.close()
        except sqlite3.Error as exc:
            out["error"] = "%s: %s" % (os.path.basename(path), exc)
    return out


def checkpoint_state():
    """Watermark and last row count per table, from state/*.json."""
    out = {}
    for path in glob.glob(os.path.join(STATE_DIR, "*.json")):
        try:
            with open(path, encoding="utf-8") as fh:
                data = json.load(fh)
        except (OSError, ValueError):
            continue
        if not isinstance(data, dict):
            continue
        for table, st in data.items():
            if isinstance(st, dict) and "last_sync_value" in st:
                out[table] = {
                    "watermark": st.get("last_sync_value") or "",
                    "rows": st.get("records_exported", 0),
                    "at": st.get("last_sync_time") or "",
                    "error": st.get("last_error") or "",
                }
    return out


def build_state(args):
    entities = discover_entities()
    orch = orchestrator_state(args.orchestrator)
    rabbit = rabbit_state(args.rabbit, args.rabbit_user, args.rabbit_password)
    audit = audit_state()
    checkpoints = checkpoint_state()

    pipeline = []
    for ent in entities:
        q = rabbit["queues"].get(ent["queue"], {})
        imported = audit["by_queue"].get(ent["queue"], {})
        cp = checkpoints.get(ent["table"], {})
        depth = q.get("messages", 0)
        consumers = q.get("consumers", 0)
        pipeline.append({
            "id": ent["id"],
            "table": ent["table"],
            "kind": ent["kind"],
            "queue": ent["queue"],
            "watermark": cp.get("watermark", ""),
            "checkpoint_rows": cp.get("rows", 0),
            "checkpoint_error": cp.get("error", ""),
            "depth": depth,
            "consumers": consumers,
            "publish_rate": round(q.get("publish_rate", 0.0), 2),
            "deliver_rate": round(q.get("deliver_rate", 0.0), 2),
            "imported_messages": imported.get("messages", 0),
            "imported_records": imported.get("records", 0),
            "imported_last": imported.get("last", ""),
            # The failure worth shouting about: work waiting with nobody to do it.
            "stalled": bool(depth > 0 and consumers == 0 and ent["queue"] in rabbit["queues"]),
            "known_to_broker": ent["queue"] in rabbit["queues"],
        })

    # Queues the broker knows about that no entity claims -- leftovers from
    # deleted scripts and old experiments accumulate here unnoticed.
    claimed = {e["queue"] for e in entities}
    orphans = [{"queue": name, **info}
               for name, info in sorted(rabbit["queues"].items())
               if name not in claimed and (info["messages"] > 0 or info["consumers"] > 0)]

    return {
        "pipeline": pipeline,
        "orphans": orphans,
        "jobs": orch["jobs"],
        "schedules": orch["schedules"],
        "audit_failures": audit["failures"],
        "sources": {
            "orchestrator": orch["error"],
            "rabbitmq": rabbit["error"],
            "audit": audit["error"],
        },
    }


# ─── HTTP ────────────────────────────────────────────────────────────────────

PAGE = r"""<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<title>Travel Agency — live</title>
<style>
:root{--bg:#fff;--fg:#111;--dim:#666;--line:#e3e3e3;--card:#fafafa;
      --ok:#0a7c3f;--warn:#b45309;--bad:#b91c1c;--accent:#1d4ed8}
@media(prefers-color-scheme:dark){:root{--bg:#0f1115;--fg:#e6e6e6;--dim:#8b8f98;
      --line:#242832;--card:#161a21;--ok:#3fb950;--warn:#d29922;--bad:#f85149;--accent:#58a6ff}}
*{box-sizing:border-box}
body{margin:0;padding:20px;background:var(--bg);color:var(--fg);
     font:14px/1.5 ui-sans-serif,system-ui,"Segoe UI",Roboto,sans-serif}
h1{font-size:18px;margin:0 0 2px}
h2{font-size:13px;text-transform:uppercase;letter-spacing:.06em;color:var(--dim);
   margin:26px 0 8px;font-weight:600}
.sub{color:var(--dim);font-size:12px;margin-bottom:14px}
.pills{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:6px}
.pill{padding:3px 9px;border-radius:99px;font-size:12px;border:1px solid var(--line)}
.pill.ok{color:var(--ok)} .pill.bad{color:var(--bad);border-color:var(--bad)}
.wrap{overflow-x:auto;border:1px solid var(--line);border-radius:8px}
table{border-collapse:collapse;width:100%;min-width:900px}
th,td{padding:7px 10px;text-align:left;border-bottom:1px solid var(--line);
      white-space:nowrap;font-variant-numeric:tabular-nums}
th{font-size:11px;text-transform:uppercase;letter-spacing:.05em;color:var(--dim);font-weight:600}
tr:last-child td{border-bottom:none}
td.num{text-align:right}
.mono{font-family:ui-monospace,"Cascadia Code",Consolas,monospace;font-size:12px}
.dim{color:var(--dim)}
.ok{color:var(--ok)} .warn{color:var(--warn)} .bad{color:var(--bad)}
.row-bad{background:color-mix(in srgb,var(--bad) 10%,transparent)}
.note{border:1px solid var(--line);border-left:3px solid var(--warn);background:var(--card);
      padding:9px 12px;border-radius:6px;margin-bottom:8px;font-size:13px}
.note.bad{border-left-color:var(--bad)}
.tag{font-size:11px;padding:1px 6px;border-radius:4px;border:1px solid var(--line);color:var(--dim)}
</style></head><body>
<h1>Travel Agency — live</h1>
<div class="sub">Read-only view. Refreshes every 2s. <span id="clock"></span></div>
<div class="pills" id="sources"></div>
<div id="alerts"></div>

<h2>Pipeline</h2>
<div class="wrap"><table id="pipeline">
<thead><tr>
  <th>Entity</th><th>Watermark</th><th class="num">Queue</th><th class="num">Cons.</th>
  <th class="num">In/s</th><th class="num">Out/s</th><th class="num">Imported msgs</th>
  <th class="num">Imported rows</th>
</tr></thead><tbody></tbody></table></div>

<h2>Schedules</h2>
<div class="wrap"><table id="schedules">
<thead><tr><th>Schedule</th><th>Every</th><th>Last run</th><th>Last fired</th><th>Next</th></tr></thead>
<tbody></tbody></table></div>

<h2>Recent runs</h2>
<div class="wrap"><table id="jobs">
<thead><tr><th>Finished</th><th>Scenario</th><th>Status</th><th class="num">Rows</th><th>Detail</th></tr></thead>
<tbody></tbody></table></div>

<script>
const $ = s => document.querySelector(s);
const el = (t, cls, txt) => { const n = document.createElement(t);
  if (cls) n.className = cls; if (txt !== undefined) n.textContent = txt; return n; };
const short = s => s ? String(s).replace('T',' ').replace('Z','').slice(0,19) : '—';

function sources(s){
  const box = $('#sources'); box.innerHTML = '';
  for (const [name, err] of Object.entries(s)){
    const p = el('span', 'pill ' + (err ? 'bad' : 'ok'), name + (err ? ' — unreachable' : ''));
    if (err) p.title = err;
    box.appendChild(p);
  }
}

function alerts(d){
  const box = $('#alerts'); box.innerHTML = '';
  const stalled = d.pipeline.filter(p => p.stalled);
  if (stalled.length){
    box.appendChild(el('div','note bad',
      'Stalled import: ' + stalled.map(p => p.queue + ' (' + p.depth + ' waiting, no consumer)').join(', ') +
      ' — start the listener for that node.'));
  }
  for (const o of d.orphans){
    box.appendChild(el('div','note',
      'Unclaimed queue ' + o.queue + ': ' + o.messages + ' message(s), ' +
      o.consumers + ' consumer(s). No workflow step sends here.'));
  }
  for (const f of d.audit_failures){
    box.appendChild(el('div','note bad',
      'Import failed on ' + f.queue + ' at ' + short(f.at) + ': ' + (f.error || f.status)));
  }
  for (const p of d.pipeline){
    if (p.checkpoint_error) box.appendChild(el('div','note bad',
      p.table + ' — last export failed, checkpoint held: ' + p.checkpoint_error));
  }
}

function pipeline(rows){
  const tb = $('#pipeline tbody'); tb.innerHTML = '';
  for (const p of rows){
    const tr = el('tr', p.stalled ? 'row-bad' : '');
    const name = el('td');
    name.appendChild(el('span', '', p.table + ' '));
    name.appendChild(el('span', 'tag', p.kind === 'full' ? 'full reload' : 'incremental'));
    tr.appendChild(name);
    tr.appendChild(el('td','mono dim', p.kind === 'full' ? '— (no timestamp column)' : short(p.watermark)));
    tr.appendChild(el('td','num' + (p.depth ? '' : ' dim'), p.known_to_broker ? p.depth : '—'));
    tr.appendChild(el('td','num ' + (p.stalled ? 'bad' : p.consumers ? 'ok' : 'dim'),
                      p.known_to_broker ? p.consumers : '—'));
    tr.appendChild(el('td','num dim', p.publish_rate || ''));
    tr.appendChild(el('td','num dim', p.deliver_rate || ''));
    tr.appendChild(el('td','num', p.imported_messages || ''));
    tr.appendChild(el('td','num', p.imported_records || ''));
    tb.appendChild(tr);
  }
}

function schedules(rows){
  const tb = $('#schedules tbody'); tb.innerHTML = '';
  for (const s of rows){
    const tr = el('tr');
    tr.appendChild(el('td','', s.id));
    tr.appendChild(el('td','mono dim', s.cron));
    // Passed through as-is since v1.20.4, where the orchestrator started
    // writing the outcome back. Before that it stamped "running" at dispatch
    // and never revisited it, so this column relabelled the raw value to say
    // what it actually meant. Now "running" is a genuine in-flight run and
    // "failed" a genuine failure, and relabelling would be the lie.
    tr.appendChild(el('td', s.last_status === 'failed' ? 'bad'
                         : s.last_status === 'running' ? 'warn' : 'ok',
                      s.last_status || '—'));
    tr.appendChild(el('td','mono dim', short(s.last_run)));
    tr.appendChild(el('td','mono dim', short(s.next_run)));
    tb.appendChild(tr);
  }
}

function jobs(rows){
  const tb = $('#jobs tbody'); tb.innerHTML = '';
  for (const j of rows){
    const tr = el('tr');
    tr.appendChild(el('td','mono dim', short(j.finished_at)));
    tr.appendChild(el('td','', j.scenario));
    tr.appendChild(el('td', j.status === 'done' ? 'ok' : j.status === 'failed' ? 'bad' : 'warn', j.status));
    tr.appendChild(el('td','num', j.rows || ''));
    tr.appendChild(el('td','dim',
      j.failed_steps.length ? 'failed: ' + j.failed_steps.join(', ')
      : j.idle ? j.idle + ' table(s) unchanged' : ''));
    tb.appendChild(tr);
  }
}

async function tick(){
  try {
    const d = await (await fetch('/api/state')).json();
    sources(d.sources); alerts(d); pipeline(d.pipeline);
    schedules(d.schedules); jobs(d.jobs);
    $('#clock').textContent = 'updated ' + new Date().toLocaleTimeString();
  } catch (e) {
    $('#clock').textContent = 'dashboard unreachable';
  }
}
tick(); setInterval(tick, 2000);
</script></body></html>
"""


def make_handler(args):
    class Handler(BaseHTTPRequestHandler):
        def _send(self, code, body, ctype):
            data = body.encode("utf-8")
            self.send_response(code)
            self.send_header("Content-Type", ctype + "; charset=utf-8")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def do_GET(self):  # noqa: N802 - BaseHTTPRequestHandler's naming
            if self.path.startswith("/api/state"):
                self._send(200, json.dumps(build_state(args)), "application/json")
            elif self.path in ("/", "/index.html"):
                self._send(200, PAGE, "text/html")
            else:
                self._send(404, "not found", "text/plain")

        def log_message(self, *_args):
            pass  # one line per poll, twice a second, is not information

    return Handler


def main():
    p = argparse.ArgumentParser(description="Travel Agency live dashboard (read-only)")
    p.add_argument("--port", type=int, default=8100)
    p.add_argument("--orchestrator", default="http://localhost:8099")
    p.add_argument("--rabbit", default="http://localhost:15672")
    p.add_argument("--rabbit-user", default="tdtp")
    p.add_argument("--rabbit-password", default="tdtp")
    args = p.parse_args()

    entities = discover_entities()
    print("Travel Agency dashboard")
    print("  url          : http://localhost:%d" % args.port)
    print("  orchestrator : %s" % args.orchestrator)
    print("  rabbitmq     : %s" % args.rabbit)
    print("  entities     : %d discovered from workflows/" % len(entities))
    print("  Ctrl+C to stop")

    ThreadingHTTPServer(("127.0.0.1", args.port), make_handler(args)).serve_forever()


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\nStopped.")
