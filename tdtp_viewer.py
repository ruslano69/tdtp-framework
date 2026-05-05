#!/usr/bin/env python3
"""
TDTP HTML Viewer with server-side pagination via libtdtp.

Usage:
    python tdtp_viewer.py <file.tdtp.xml> [--port 8080] [--page-size 200] [--open]
"""
from __future__ import annotations

import argparse
import html
import os
import sys
import threading
import webbrowser
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
sys.stderr.reconfigure(encoding="utf-8", errors="replace")

# Locate bindings relative to this script
_HERE = Path(__file__).resolve().parent
_BINDINGS = _HERE / "bindings" / "python"
if _BINDINGS.exists():
    sys.path.insert(0, str(_BINDINGS))

try:
    from tdtp import TDTPClientJSON
except ImportError as e:
    print(f"ERROR: cannot import tdtp bindings: {e}")
    print(f"  Make sure libtdtp.dll is in {_BINDINGS / 'tdtp'}")
    sys.exit(1)

# ---------------------------------------------------------------------------
# Global viewer state
# ---------------------------------------------------------------------------

_client: TDTPClientJSON = TDTPClientJSON()
_data: dict = {}
_file_path: str = ""
_page_size: int = 200


# ---------------------------------------------------------------------------
# Rendering
# ---------------------------------------------------------------------------

TYPE_CSS = {
    "integer": "num", "decimal": "num", "real": "num",
    "boolean": "bool",
    "blob": "blob",
}

BADGE = {
    "reference": "badge-reference",
    "response":  "badge-response",
    "request":   "badge-request",
    "alarm":     "badge-alarm",
}


def _cell(val: str, ftype: str) -> str:
    if val == "":
        return '<td><span class="null">NULL</span></td>'
    ft = ftype.lower()
    if ft in ("integer", "decimal", "real"):
        return f'<td class="num">{html.escape(val)}</td>'
    if ft == "boolean":
        cls = "btrue" if val in ("1", "true", "True") else "bfalse"
        return f'<td><span class="{cls}">{html.escape(val)}</span></td>'
    if ft == "blob":
        return '<td><span class="null">&lt;binary&gt;</span></td>'
    return f"<td>{html.escape(val)}</td>"


def _pagination(page: int, total_pages: int, where: str) -> str:
    if total_pages <= 1:
        return ""

    def link(p: int, label: str, disabled: bool = False) -> str:
        if disabled:
            return f'<span class="pg-btn disabled">{label}</span>'
        w = html.escape(where, quote=True)
        return f'<a class="pg-btn" href="/?page={p}&where={w}">{label}</a>'

    parts = [link(1, "«", page == 1), link(page - 1, "‹", page == 1)]

    # window of pages around current
    lo = max(1, page - 3)
    hi = min(total_pages, page + 3)
    if lo > 1:
        parts.append('<span class="pg-dots">…</span>')
    for p in range(lo, hi + 1):
        if p == page:
            parts.append(f'<span class="pg-btn current">{p}</span>')
        else:
            parts.append(link(p, str(p)))
    if hi < total_pages:
        parts.append('<span class="pg-dots">…</span>')

    parts += [link(page + 1, "›", page == total_pages),
              link(total_pages, "»", page == total_pages)]
    return '<div class="pagination">' + "".join(parts) + "</div>"


def render_page(page: int, where: str) -> str:
    offset = (page - 1) * _page_size

    if where:
        result  = _client.J_filter(_data, where, limit=_page_size, offset=offset)
        ctx     = result.get("query_context", {})
        total   = ctx.get("total_records",   len(_data["data"]))
        matched = ctx.get("matched_records", total)
        rows    = result["data"]
        schema  = result["schema"]["Fields"]
        header  = result["header"]
    else:
        all_rows = _data["data"]
        total    = len(all_rows)
        matched  = total
        rows     = all_rows[offset: offset + _page_size]
        schema   = _data["schema"]["Fields"]
        header   = _data["header"]

    total_pages = max(1, (matched + _page_size - 1) // _page_size)
    table   = html.escape(header.get("table_name", ""))
    msg_type = header.get("type", "reference").lower()
    badge   = BADGE.get(msg_type, "badge-reference")
    fname   = html.escape(Path(_file_path).name)

    pager = _pagination(page, total_pages, where)

    # --- filter bar ---
    where_val = html.escape(where, quote=True)
    filter_bar = f"""
<form class="filter-bar" method="get" action="/">
  <input type="hidden" name="page" value="1">
  <input class="where-input" name="where" value="{where_val}"
         placeholder="TDTQL WHERE clause — e.g.  Balance &gt; 1000 AND City = 'Kyiv'">
  <button type="submit">Filter</button>
  {"<a class='clear-btn' href='/'>Clear</a>" if where else ""}
</form>"""

    # --- schema rows ---
    schema_rows = ""
    for i, f in enumerate(schema):
        t = f["Type"].upper()
        if f.get("Length") and f["Length"] > 0:
            t += f'({f["Length"]})'
        elif f.get("Precision") and f["Precision"] > 0:
            t += f'({f["Precision"]}' + (f',{f["Scale"]}' if f.get("Scale") else "") + ")"
        attrs = ""
        if f.get("Key"):
            attrs += '<span class="badge badge-key">PK</span> '
        if f.get("ReadOnly"):
            attrs += '<span class="badge badge-type">readonly</span>'
        schema_rows += (
            f'<tr><td class="rn">{i+1}</td>'
            f'<td class="fname">{html.escape(f["Name"])}</td>'
            f'<td><span class="badge badge-type">{html.escape(t)}</span></td>'
            f'<td>{attrs}</td></tr>'
        )

    # --- data rows ---
    data_rows = ""
    for ri, row in enumerate(rows):
        cells = "".join(
            _cell(row[ci] if ci < len(row) else "", schema[ci]["Type"])
            for ci in range(len(schema))
        )
        data_rows += f'<tr><td class="rn">{offset + ri + 1}</td>{cells}</tr>'

    # --- status line ---
    if where:
        status = (f"Filtered: <strong>{matched}</strong> of "
                  f"<strong>{total}</strong> rows &nbsp;·&nbsp; "
                  f"Page <strong>{page}</strong> of <strong>{total_pages}</strong> "
                  f"&nbsp;·&nbsp; {_page_size} rows/page")
    else:
        status = (f"<strong>{total}</strong> rows &nbsp;·&nbsp; "
                  f"Page <strong>{page}</strong> of <strong>{total_pages}</strong> "
                  f"&nbsp;·&nbsp; {_page_size} rows/page")

    col_headers = '<th class="rn">#</th>' + "".join(
        f'<th{"  class=\"key-col\"" if f.get("Key") else ""}>'
        f'{html.escape(f["Name"])}<br>'
        f'<small>{html.escape(f["Type"].lower())}</small></th>'
        for f in schema
    )

    return f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>TDTP Viewer — {table}</title>
<style>
*{{box-sizing:border-box;margin:0;padding:0}}
body{{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;
     background:#0f1117;color:#e2e8f0;min-height:100vh;padding:20px}}
.wrap{{max-width:1800px;margin:0 auto}}
/* header */
.hdr{{background:linear-gradient(135deg,#1e293b,#0f172a);border:1px solid #334155;
      border-radius:12px;padding:20px 24px;margin-bottom:16px}}
.hdr-top{{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin-bottom:12px}}
.tname{{font-size:24px;font-weight:700;color:#f1f5f9}}
.meta{{display:flex;gap:20px;flex-wrap:wrap;font-size:12px;color:#64748b}}
.meta span{{display:flex;flex-direction:column;gap:2px}}
.meta b{{color:#94a3b8;font-size:11px;text-transform:uppercase;letter-spacing:.05em}}
/* badges */
.badge{{display:inline-flex;align-items:center;padding:3px 9px;border-radius:20px;
        font-size:11px;font-weight:600}}
.badge-reference{{background:#1e3a5f;color:#60a5fa}}
.badge-response{{background:#1a3a2a;color:#34d399}}
.badge-request{{background:#3a2a1a;color:#fb923c}}
.badge-alarm{{background:#3a1a1a;color:#f87171}}
.badge-key{{background:#2d1b69;color:#a78bfa}}
.badge-type{{background:#1e293b;color:#94a3b8;font-family:monospace}}
/* filter */
.filter-bar{{display:flex;gap:8px;margin-bottom:14px;align-items:center}}
.where-input{{flex:1;background:#1e293b;border:1px solid #334155;border-radius:8px;
              color:#e2e8f0;padding:8px 14px;font-size:13px;font-family:monospace;
              outline:none}}
.where-input:focus{{border-color:#60a5fa}}
.filter-bar button{{background:#1d4ed8;color:#fff;border:none;border-radius:8px;
                    padding:8px 18px;cursor:pointer;font-size:13px;font-weight:600}}
.filter-bar button:hover{{background:#2563eb}}
.clear-btn{{color:#94a3b8;font-size:13px;text-decoration:none;padding:8px 12px}}
.clear-btn:hover{{color:#e2e8f0}}
/* card */
.card{{background:#1e293b;border:1px solid #334155;border-radius:12px;
       margin-bottom:16px;overflow:hidden}}
.card-hdr{{padding:12px 18px;border-bottom:1px solid #334155;font-size:13px;
           font-weight:600;color:#94a3b8;background:#0f172a;
           display:flex;align-items:center;gap:10px}}
.pill{{background:#334155;color:#94a3b8;padding:2px 8px;border-radius:10px;
       font-size:11px;font-weight:600}}
/* schema table */
.stbl{{width:100%;border-collapse:collapse}}
.stbl th{{padding:9px 14px;font-size:11px;font-weight:600;color:#475569;
          text-transform:uppercase;letter-spacing:.05em;
          border-bottom:1px solid #334155;background:#0f172a;text-align:left}}
.stbl td{{padding:8px 14px;font-size:13px;border-bottom:1px solid #1e293b}}
.stbl tr:last-child td{{border-bottom:none}}
.stbl tr:hover td{{background:#263244}}
.fname{{font-family:monospace;font-weight:600;color:#e2e8f0}}
/* data table */
.dtbl-wrap{{overflow-x:auto}}
.dtbl{{width:100%;border-collapse:collapse;font-size:13px}}
.dtbl th{{padding:9px 12px;font-size:11px;font-weight:600;color:#475569;
          text-transform:uppercase;letter-spacing:.04em;
          border-bottom:2px solid #334155;background:#0f172a;
          white-space:nowrap;position:sticky;top:0;z-index:10;text-align:left}}
.dtbl th.key-col{{color:#a78bfa}}
.dtbl td{{padding:7px 12px;border-bottom:1px solid #1e293b;
          font-family:monospace;color:#cbd5e1;
          max-width:320px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}}
.dtbl tr:hover td{{background:#1e2d42;cursor:default}}
.dtbl tr:nth-child(even) td{{background:#18222f}}
.dtbl tr:nth-child(even):hover td{{background:#1e2d42}}
.rn{{color:#475569;text-align:right;user-select:none;font-size:11px;width:50px}}
.null{{color:#475569;font-style:italic}}
.num{{color:#60a5fa;text-align:right}}
.btrue{{color:#34d399}}.bfalse{{color:#f87171}}
/* status + pagination */
.statusbar{{display:flex;justify-content:space-between;align-items:center;
            flex-wrap:wrap;gap:12px;
            padding:10px 18px;background:#0f172a;border-top:1px solid #334155;
            font-size:12px;color:#64748b}}
.pagination{{display:flex;gap:4px;flex-wrap:wrap}}
.pg-btn{{display:inline-flex;align-items:center;justify-content:center;
         min-width:32px;height:32px;padding:0 8px;
         background:#1e293b;border:1px solid #334155;border-radius:6px;
         color:#94a3b8;text-decoration:none;font-size:13px}}
.pg-btn:hover{{background:#263244;color:#e2e8f0}}
.pg-btn.current{{background:#1d4ed8;border-color:#1d4ed8;color:#fff;font-weight:700}}
.pg-btn.disabled{{opacity:.35;pointer-events:none}}
.pg-dots{{display:inline-flex;align-items:center;color:#475569;padding:0 4px}}
.footer{{text-align:center;padding:16px;font-size:11px;color:#334155}}
.footer a{{color:#475569;text-decoration:none}}
</style>
</head>
<body>
<div class="wrap">

<div class="hdr">
  <div class="hdr-top">
    <span class="tname">{table}</span>
    <span class="badge {badge}">{html.escape(msg_type.upper())}</span>
  </div>
  <div class="meta">
    <span><b>File</b>{fname}</span>
    <span><b>Fields</b>{len(schema)}</span>
    <span><b>Total rows</b>{total}</span>
    <span><b>Page size</b>{_page_size}</span>
  </div>
</div>

{filter_bar}

<div class="card">
  <div class="card-hdr">Schema <span class="pill">{len(schema)} fields</span></div>
  <table class="stbl">
    <thead><tr><th>#</th><th>Field</th><th>Type</th><th>Attrs</th></tr></thead>
    <tbody>{schema_rows}</tbody>
  </table>
</div>

<div class="card">
  <div class="card-hdr">Data <span class="pill">{status}</span></div>
  {pager.replace('class="pagination"','class="pagination" style="padding:10px 18px 0"') if pager else ""}
  <div class="dtbl-wrap">
    <table class="dtbl">
      <thead><tr>{col_headers}</tr></thead>
      <tbody>{data_rows}</tbody>
    </table>
  </div>
  <div class="statusbar">
    <span>{status}</span>
    {pager}
  </div>
</div>

<div class="footer">
  <a href="https://github.com/ruslano69/tdtp-framework">TDTP Framework</a>
  &mdash; Table Data Transfer Protocol
</div>
</div>
</body>
</html>"""


# ---------------------------------------------------------------------------
# HTTP handler
# ---------------------------------------------------------------------------

class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        pass  # тихий режим

    def do_GET(self):
        parsed = urlparse(self.path)
        if parsed.path != "/":
            self.send_error(404)
            return

        qs = parse_qs(parsed.query)
        try:
            page = max(1, int(qs.get("page", ["1"])[0]))
        except ValueError:
            page = 1
        where = qs.get("where", [""])[0].strip()

        try:
            body = render_page(page, where)
            encoded = body.encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(encoded)))
            self.end_headers()
            self.wfile.write(encoded)
        except Exception as e:
            err = f"<pre style='color:red;padding:20px'>{html.escape(str(e))}</pre>"
            enc = err.encode()
            self.send_response(500)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(enc)))
            self.end_headers()
            self.wfile.write(enc)


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

def main():
    global _data, _file_path, _page_size

    ap = argparse.ArgumentParser(description="TDTP viewer with pagination")
    ap.add_argument("file", help="Path to .tdtp.xml file")
    ap.add_argument("--port", type=int, default=8080)
    ap.add_argument("--page-size", type=int, default=200)
    ap.add_argument("--open", action="store_true", dest="open_browser")
    args = ap.parse_args()

    _file_path = args.file
    _page_size = args.page_size

    if not Path(_file_path).exists():
        print(f"ERROR: file not found: {_file_path}")
        sys.exit(1)

    print(f"Loading {_file_path} ...", end=" ", flush=True)
    _data = _client.J_read(_file_path)
    total = len(_data["data"])
    table = _data["header"].get("table_name", "?")
    fields = len(_data["schema"]["Fields"])
    print(f"OK  ({total} rows, {fields} fields, table={table})")

    url = f"http://localhost:{args.port}/"
    print(f"Serving at {url}   (Ctrl+C to stop)")

    server = HTTPServer(("localhost", args.port), Handler)

    if args.open_browser:
        threading.Timer(0.3, lambda: webbrowser.open(url)).start()

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nStopped.")


if __name__ == "__main__":
    main()
