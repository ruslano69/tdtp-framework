package main

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
	"github.com/ruslano69/tdtp-framework/pkg/etl"
)

// ─────────────────────────────────────────────────────────────────────────────
// Data model
// ─────────────────────────────────────────────────────────────────────────────

// Dataset — загруженный источник или вычисленный вид
type Dataset struct {
	Name   string
	IsView bool
	Desc   string
	Type   string // "tdtp" / "postgres" / "sqlite" / …
	Packet *packet.DataPacket
}

// Server — HTTP сервер tdtpserve
type Server struct {
	cfg      *ServeConfig
	datasets map[string]*Dataset
	order    []string // порядок для отображения в UI
	startedAt time.Time
}

// ─────────────────────────────────────────────────────────────────────────────
// Startup: load all sources and views
// ─────────────────────────────────────────────────────────────────────────────

func newServer(ctx context.Context, cfg *ServeConfig) (*Server, error) {
	srv := &Server{
		cfg:       cfg,
		datasets:  make(map[string]*Dataset),
		startedAt: time.Now(),
	}

	fmt.Printf("tdtpserve: loading %d source(s)...\n", len(cfg.Sources))

	// 1. Load all sources via etl.Loader (handles tdtp files + DB adapters)
	loader := etl.NewLoader(cfg.Sources, etl.ErrorHandlingConfig{OnSourceError: "fail"})
	sourcesData, err := loader.LoadAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading sources: %w", err)
	}

	// Build a name→type index from config
	sourceTypes := make(map[string]string, len(cfg.Sources))
	for _, s := range cfg.Sources {
		sourceTypes[s.Name] = s.Type
	}

	for _, sd := range sourcesData {
		if sd.Error != nil {
			return nil, fmt.Errorf("source %q: %w", sd.SourceName, sd.Error)
		}
		rows := 0
		if sd.Packet != nil {
			rows = len(sd.Packet.Data.Rows)
		}
		fmt.Printf("  [%s] %s — %d rows, %d fields\n",
			sourceTypes[sd.SourceName], sd.SourceName, rows, len(sd.Packet.Schema.Fields))

		srv.datasets[sd.SourceName] = &Dataset{
			Name:   sd.SourceName,
			IsView: false,
			Type:   sourceTypes[sd.SourceName],
			Packet: sd.Packet,
		}
		srv.order = append(srv.order, sd.SourceName)
	}

	// 2. Compute views in a SQLite :memory: workspace (JOIN over sources)
	if len(cfg.Views) > 0 {
		fmt.Printf("tdtpserve: computing %d view(s)...\n", len(cfg.Views))

		workspace, err := etl.NewWorkspace(ctx)
		if err != nil {
			return nil, fmt.Errorf("workspace init: %w", err)
		}
		defer workspace.Close(ctx) //nolint:errcheck

		// Populate workspace tables from source packets
		for _, sd := range sourcesData {
			if sd.Error != nil || sd.Packet == nil {
				continue
			}
			if err := workspace.CreateTable(ctx, sd.TableName, sd.Packet.Schema.Fields); err != nil {
				return nil, fmt.Errorf("workspace create table %q: %w", sd.TableName, err)
			}
			if err := workspace.LoadData(ctx, sd.TableName, sd.Packet); err != nil {
				return nil, fmt.Errorf("workspace load %q: %w", sd.TableName, err)
			}
		}

		// Execute each view SQL and cache result
		for _, v := range cfg.Views {
			pkt, err := workspace.ExecuteSQL(ctx, v.SQL, v.Name)
			if err != nil {
				return nil, fmt.Errorf("view %q: %w", v.Name, err)
			}
			fmt.Printf("  [view] %s — %d rows, %d fields\n",
				v.Name, len(pkt.Data.Rows), len(pkt.Schema.Fields))

			srv.datasets[v.Name] = &Dataset{
				Name:   v.Name,
				IsView: true,
				Desc:   v.Description,
				Type:   "view",
				Packet: pkt,
			}
			srv.order = append(srv.order, v.Name)
		}
	}

	return srv, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP server
// ─────────────────────────────────────────────────────────────────────────────

func runServer(cfg *ServeConfig) error {
	ctx := context.Background()

	srv, err := newServer(ctx, cfg)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleIndex)
	mux.HandleFunc("/data/", srv.handleData)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	fmt.Printf("\ntdtpserve ready → http://localhost%s\n", addr)
	fmt.Printf("  %d source(s), %d view(s)\n", srv.sourceCount(), srv.viewCount())

	return http.ListenAndServe(addr, mux)
}

func (s *Server) sourceCount() int {
	n := 0
	for _, d := range s.datasets {
		if !d.IsView {
			n++
		}
	}
	return n
}

func (s *Server) viewCount() int {
	n := 0
	for _, d := range s.datasets {
		if d.IsView {
			n++
		}
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// Handlers
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.renderIndex(w)
}

func (s *Server) handleData(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/data/")
	name = strings.TrimSuffix(name, "/")
	if name == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	ds, ok := s.datasets[name]
	if !ok {
		http.Error(w, "dataset not found: "+name, http.StatusNotFound)
		return
	}

	q := r.URL.Query()
	whereExpr := q.Get("where")
	orderBy := q.Get("order_by")
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	// Apply TDTQL filtering
	allRows := extractRows(ds.Packet)
	var filterErr string

	if whereExpr != "" || orderBy != "" || limit > 0 || offset > 0 {
		query, err := buildQuery(whereExpr, orderBy, limit, offset)
		if err != nil {
			filterErr = err.Error()
		} else if query != nil {
			exec := tdtql.NewExecutor()
			result, err := exec.Execute(query, allRows, ds.Packet.Schema)
			if err != nil {
				filterErr = err.Error()
			} else {
				allRows = result.FilteredRows
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.renderData(w, ds, allRows, whereExpr, orderBy, limit, offset, filterErr)
}

// extractRows gets all rows from a DataPacket as [][]string
func extractRows(pkt *packet.DataPacket) [][]string {
	p := packet.NewParser()
	rows := make([][]string, 0, len(pkt.Data.Rows))
	for _, row := range pkt.Data.Rows {
		rows = append(rows, p.GetRowValues(row))
	}
	return rows
}

// ─────────────────────────────────────────────────────────────────────────────
// Query building (WHERE / ORDER BY / LIMIT / OFFSET → packet.Query)
// ─────────────────────────────────────────────────────────────────────────────

func buildQuery(where, orderBy string, limit, offset int) (*packet.Query, error) {
	if where == "" && orderBy == "" && limit == 0 && offset == 0 {
		return nil, nil
	}

	q := packet.NewQuery()

	if where != "" {
		filters, err := parseWhere(where)
		if err != nil {
			return nil, fmt.Errorf("WHERE: %w", err)
		}
		q.Filters = filters
	}

	if orderBy != "" {
		ob, err := parseOrderBy(orderBy)
		if err != nil {
			return nil, fmt.Errorf("ORDER BY: %w", err)
		}
		q.OrderBy = ob
	}

	if limit > 0 {
		q.Limit = limit
	}
	if offset > 0 {
		q.Offset = offset
	}

	return q, nil
}

func parseWhere(where string) (*packet.Filters, error) {
	where = strings.TrimSpace(where)

	if strings.Contains(where, " AND ") {
		parts := strings.Split(where, " AND ")
		filters := make([]packet.Filter, 0, len(parts))
		for _, p := range parts {
			f, err := parseSimpleFilter(strings.TrimSpace(p))
			if err != nil {
				return nil, err
			}
			filters = append(filters, f)
		}
		return &packet.Filters{And: &packet.LogicalGroup{Filters: filters}}, nil
	}

	if strings.Contains(where, " OR ") {
		parts := strings.Split(where, " OR ")
		filters := make([]packet.Filter, 0, len(parts))
		for _, p := range parts {
			f, err := parseSimpleFilter(strings.TrimSpace(p))
			if err != nil {
				return nil, err
			}
			filters = append(filters, f)
		}
		return &packet.Filters{Or: &packet.LogicalGroup{Filters: filters}}, nil
	}

	f, err := parseSimpleFilter(where)
	if err != nil {
		return nil, err
	}
	return &packet.Filters{And: &packet.LogicalGroup{Filters: []packet.Filter{f}}}, nil
}

func parseSimpleFilter(cond string) (packet.Filter, error) {
	cond = strings.TrimSpace(cond)
	ops := []string{">=", "<=", "!=", "=", ">", "<", " LIKE ", " IN ", " BETWEEN ", " IS NOT NULL", " IS NULL"}

	for _, op := range ops {
		idx := strings.Index(strings.ToUpper(cond), op)
		if idx == -1 {
			continue
		}
		field := strings.TrimSpace(cond[:idx])
		if op == " IS NULL" || op == " IS NOT NULL" {
			return packet.Filter{Field: field, Operator: strings.TrimSpace(op)}, nil
		}
		valuePart := strings.TrimSpace(cond[idx+len(op):])
		var value, value2 string
		if strings.Contains(strings.ToUpper(op), "BETWEEN") {
			parts := strings.SplitN(valuePart, " AND ", 2)
			if len(parts) != 2 {
				return packet.Filter{}, fmt.Errorf("BETWEEN needs two values: %s", cond)
			}
			value = strings.Trim(strings.TrimSpace(parts[0]), "'\"")
			value2 = strings.Trim(strings.TrimSpace(parts[1]), "'\"")
		} else {
			value = strings.Trim(valuePart, "'\"")
		}
		tdtpOp := map[string]string{
			"=": "eq", "!=": "ne", ">": "gt", "<": "lt",
			">=": "gte", "<=": "lte", "LIKE": "like",
			"IN": "in", "BETWEEN": "between",
			"IS NULL": "is_null", "IS NOT NULL": "is_not_null",
		}[strings.TrimSpace(op)]
		if tdtpOp == "" {
			tdtpOp = strings.ToLower(strings.TrimSpace(op))
		}
		return packet.Filter{Field: field, Operator: tdtpOp, Value: value, Value2: value2}, nil
	}
	return packet.Filter{}, fmt.Errorf("cannot parse condition: %s", cond)
}

func parseOrderBy(orderBy string) (*packet.OrderBy, error) {
	parts := strings.Split(orderBy, ",")
	if len(parts) == 1 {
		tokens := strings.Fields(strings.TrimSpace(parts[0]))
		if len(tokens) == 0 {
			return nil, fmt.Errorf("empty ORDER BY")
		}
		dir := "ASC"
		if len(tokens) > 1 && strings.ToUpper(tokens[1]) == "DESC" {
			dir = "DESC"
		}
		return &packet.OrderBy{Field: tokens[0], Direction: dir}, nil
	}
	fields := make([]packet.OrderField, 0, len(parts))
	for _, p := range parts {
		tokens := strings.Fields(strings.TrimSpace(p))
		if len(tokens) == 0 {
			continue
		}
		dir := "ASC"
		if len(tokens) > 1 && strings.ToUpper(tokens[1]) == "DESC" {
			dir = "DESC"
		}
		fields = append(fields, packet.OrderField{Name: tokens[0], Direction: dir})
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("invalid ORDER BY: %s", orderBy)
	}
	return &packet.OrderBy{Fields: fields}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// HTML rendering — index page
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) renderIndex(w http.ResponseWriter) {
	var b strings.Builder

	b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + html.EscapeString(s.cfg.Server.Name) + `</title>
` + commonCSS() + `
<style>
  .grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(300px,1fr)); gap:16px; }
  .card-link { text-decoration:none; color:inherit; display:block; }
  .src-card {
    background:#1e293b; border:1px solid #334155; border-radius:12px;
    padding:20px; transition:border-color .15s, transform .1s;
    cursor:pointer;
  }
  .src-card:hover { border-color:#3b82f6; transform:translateY(-1px); }
  .src-card.is-view { border-color:#334155; }
  .src-card.is-view:hover { border-color:#8b5cf6; }
  .card-top { display:flex; align-items:center; gap:12px; margin-bottom:12px; }
  .card-icon {
    width:36px; height:36px; border-radius:8px; display:flex; align-items:center;
    justify-content:center; font-size:17px; flex-shrink:0;
  }
  .icon-db   { background:#1e3a5f; }
  .icon-file { background:#1a3a2a; }
  .icon-view { background:#2d1b69; }
  .card-name { font-size:16px; font-weight:700; color:#f1f5f9; }
  .card-meta { display:flex; gap:8px; flex-wrap:wrap; margin-top:8px; }
  .tag {
    font-size:11px; font-weight:600; padding:2px 8px; border-radius:10px;
    background:#1e293b; color:#94a3b8; border:1px solid #334155;
  }
  .tag-rows  { color:#34d399; border-color:#1a3a2a; background:#0d2019; }
  .tag-type  { color:#60a5fa; border-color:#1e3a5f; background:#0d1f3c; }
  .tag-view  { color:#a78bfa; border-color:#2d1b69; background:#1a0f3c; }
  .card-desc { font-size:12px; color:#64748b; margin-top:8px; font-style:italic; }
  .section-title {
    font-size:12px; font-weight:700; color:#475569;
    text-transform:uppercase; letter-spacing:.06em;
    margin:24px 0 12px;
  }
</style>
</head>
<body>
<div class="container">
`)
	// Navbar
	writeNavbar(&b, s.cfg.Server.Name, "")

	// Stats row
	b.WriteString(`<div class="meta-grid" style="margin-bottom:24px;">`)
	writeMetaItem(&b, "Sources", strconv.Itoa(s.sourceCount()))
	writeMetaItem(&b, "Views", strconv.Itoa(s.viewCount()))
	writeMetaItem(&b, "Started", s.startedAt.Format("2006-01-02 15:04:05"))
	b.WriteString(`</div>`)

	// Sources
	sources := make([]*Dataset, 0)
	views := make([]*Dataset, 0)
	for _, name := range s.order {
		d := s.datasets[name]
		if d.IsView {
			views = append(views, d)
		} else {
			sources = append(sources, d)
		}
	}

	if len(sources) > 0 {
		b.WriteString(`<div class="section-title">Sources</div><div class="grid">`)
		for _, d := range sources {
			writeSourceCard(&b, d)
		}
		b.WriteString(`</div>`)
	}

	if len(views) > 0 {
		b.WriteString(`<div class="section-title">Views</div><div class="grid">`)
		for _, d := range views {
			writeSourceCard(&b, d)
		}
		b.WriteString(`</div>`)
	}

	b.WriteString(`<div class="footer">` +
		`<a href="https://github.com/ruslano69/tdtp-framework">TDTP Framework</a> &mdash; tdtpserve` +
		`</div>`)
	b.WriteString(`</div></body></html>`)

	fmt.Fprint(w, b.String())
}

func writeSourceCard(b *strings.Builder, d *Dataset) {
	rowCount := 0
	fieldCount := 0
	if d.Packet != nil {
		rowCount = len(d.Packet.Data.Rows)
		fieldCount = len(d.Packet.Schema.Fields)
	}

	iconClass := "icon-db"
	iconChar := "&#x1F5C4;"
	typeLabel := d.Type
	tagClass := "tag-type"
	if d.Type == "tdtp" {
		iconClass = "icon-file"
		iconChar = "&#x1F4C4;"
	}
	if d.Type == "tdtp-enc" {
		iconClass = "icon-file"
		iconChar = "&#x1F512;" // 🔒
		typeLabel = "tdtp-enc"
	}
	if d.IsView {
		iconClass = "icon-view"
		iconChar = "&#x1F50D;"
		typeLabel = "view"
		tagClass = "tag-view"
	}

	b.WriteString(`<a class="card-link" href="/data/` + html.EscapeString(d.Name) + `">`)
	b.WriteString(`<div class="src-card`)
	if d.IsView {
		b.WriteString(` is-view`)
	}
	b.WriteString(`">`)
	b.WriteString(`<div class="card-top">`)
	b.WriteString(`<div class="card-icon ` + iconClass + `">` + iconChar + `</div>`)
	b.WriteString(`<span class="card-name">` + html.EscapeString(d.Name) + `</span>`)
	b.WriteString(`</div>`)
	b.WriteString(`<div class="card-meta">`)
	b.WriteString(`<span class="tag ` + tagClass + `">` + html.EscapeString(typeLabel) + `</span>`)
	b.WriteString(`<span class="tag tag-rows">` + strconv.Itoa(rowCount) + ` rows</span>`)
	b.WriteString(`<span class="tag">` + strconv.Itoa(fieldCount) + ` fields</span>`)
	b.WriteString(`</div>`)
	if d.Desc != "" {
		b.WriteString(`<div class="card-desc">` + html.EscapeString(d.Desc) + `</div>`)
	}
	b.WriteString(`</div></a>`)
}

// ─────────────────────────────────────────────────────────────────────────────
// HTML rendering — data page
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) renderData(
	w http.ResponseWriter,
	ds *Dataset,
	rows [][]string,
	whereExpr, orderBy string,
	limit, offset int,
	filterErr string,
) {
	totalRows := len(ds.Packet.Data.Rows)
	schema := ds.Packet.Schema

	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + html.EscapeString(ds.Name) + ` — ` + html.EscapeString(s.cfg.Server.Name) + `</title>
` + commonCSS() + `
<style>
  .filter-bar {
    background:#1e293b; border:1px solid #334155; border-radius:12px;
    padding:16px 20px; margin-bottom:20px;
    display:flex; gap:12px; flex-wrap:wrap; align-items:flex-end;
  }
  .filter-group { display:flex; flex-direction:column; gap:4px; flex:1; min-width:180px; }
  .filter-label { font-size:11px; font-weight:600; color:#64748b; text-transform:uppercase; letter-spacing:.05em; }
  .filter-input {
    background:#0f172a; border:1px solid #334155; border-radius:6px;
    color:#e2e8f0; padding:7px 10px; font-size:13px; font-family:monospace;
    outline:none; transition:border-color .15s;
  }
  .filter-input:focus { border-color:#3b82f6; }
  .filter-input.narrow { max-width:90px; }
  .btn {
    padding:8px 18px; border-radius:6px; font-size:13px; font-weight:600;
    cursor:pointer; border:none; transition:opacity .15s;
  }
  .btn:hover { opacity:.85; }
  .btn-primary { background:#2563eb; color:#fff; }
  .btn-ghost   { background:#1e293b; color:#94a3b8; border:1px solid #334155; }
  .error-bar {
    background:#3a1a1a; border:1px solid #f87171; border-radius:8px;
    padding:10px 16px; margin-bottom:16px; color:#f87171; font-size:13px;
  }
  .data-wrapper { overflow-x:auto; }
  .data-table { width:100%; border-collapse:collapse; font-size:13px; }
  .data-table th {
    padding:10px 14px; text-align:left;
    font-size:11px; font-weight:600; color:#475569;
    text-transform:uppercase; letter-spacing:.04em;
    border-bottom:2px solid #334155; background:#0f172a;
    white-space:nowrap; position:sticky; top:0; z-index:10;
  }
  .data-table th.key-col { color:#a78bfa; }
  .data-table td {
    padding:8px 14px; border-bottom:1px solid #1e293b;
    font-family:monospace; color:#cbd5e1;
    max-width:320px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;
  }
  .data-table tr:hover td { background:#1e2d42; }
  .data-table tr:nth-child(even) td { background:#18222f; }
  .data-table tr:nth-child(even):hover td { background:#1e2d42; }
  .null-val  { color:#475569; font-style:italic; }
  .num-val   { color:#60a5fa; }
  .bool-true { color:#34d399; }
  .bool-false{ color:#f87171; }
  .row-num   { color:#475569; text-align:right; user-select:none; font-size:11px; }
</style>
</head>
<body>
<div class="container">
`)

	writeNavbar(&b, s.cfg.Server.Name, ds.Name)

	// Header card
	b.WriteString(`<div class="header-card">`)
	b.WriteString(`<div class="header-top">`)
	b.WriteString(`<span class="table-name">` + html.EscapeString(ds.Name) + `</span>`)
	if ds.IsView {
		b.WriteString(`<span class="badge badge-key">VIEW</span>`)
	} else {
		b.WriteString(`<span class="badge badge-reference">` + html.EscapeString(strings.ToUpper(ds.Type)) + `</span>`)
	}
	b.WriteString(`</div>`) // header-top
	b.WriteString(`<div class="meta-grid">`)
	writeMetaItem(&b, "Total rows", strconv.Itoa(totalRows))
	writeMetaItem(&b, "Fields", strconv.Itoa(len(schema.Fields)))
	if ds.Desc != "" {
		writeMetaItem(&b, "Description", ds.Desc)
	}
	b.WriteString(`</div>`)
	b.WriteString(`</div>`) // header-card

	// Filter bar
	b.WriteString(`<form method="GET" action="/data/` + html.EscapeString(ds.Name) + `" class="filter-bar">`)
	b.WriteString(`<div class="filter-group">`)
	b.WriteString(`<label class="filter-label">WHERE</label>`)
	b.WriteString(`<input class="filter-input" name="where" placeholder="status = 'active' AND amount > 100"`)
	if whereExpr != "" {
		b.WriteString(` value="` + html.EscapeString(whereExpr) + `"`)
	}
	b.WriteString(`>`)
	b.WriteString(`</div>`)
	b.WriteString(`<div class="filter-group" style="max-width:200px;">`)
	b.WriteString(`<label class="filter-label">ORDER BY</label>`)
	b.WriteString(`<input class="filter-input" name="order_by" placeholder="created_at DESC"`)
	if orderBy != "" {
		b.WriteString(` value="` + html.EscapeString(orderBy) + `"`)
	}
	b.WriteString(`>`)
	b.WriteString(`</div>`)
	b.WriteString(`<div class="filter-group" style="max-width:90px;">`)
	b.WriteString(`<label class="filter-label">LIMIT</label>`)
	limitVal := ""
	if limit > 0 {
		limitVal = strconv.Itoa(limit)
	}
	b.WriteString(`<input class="filter-input narrow" name="limit" type="number" min="0" placeholder="all" value="` + limitVal + `">`)
	b.WriteString(`</div>`)
	b.WriteString(`<div class="filter-group" style="max-width:90px;">`)
	b.WriteString(`<label class="filter-label">OFFSET</label>`)
	offsetVal := ""
	if offset > 0 {
		offsetVal = strconv.Itoa(offset)
	}
	b.WriteString(`<input class="filter-input narrow" name="offset" type="number" min="0" placeholder="0" value="` + offsetVal + `">`)
	b.WriteString(`</div>`)
	b.WriteString(`<div style="display:flex;gap:8px;align-self:flex-end;">`)
	b.WriteString(`<button class="btn btn-primary" type="submit">Filter</button>`)
	b.WriteString(`<a class="btn btn-ghost" href="/data/` + html.EscapeString(ds.Name) + `">Clear</a>`)
	b.WriteString(`</div>`)
	b.WriteString(`</form>`)

	// Error bar
	if filterErr != "" {
		b.WriteString(`<div class="error-bar">Filter error: ` + html.EscapeString(filterErr) + `</div>`)
	}

	// Data card
	b.WriteString(`<div class="card">`)
	if len(rows) < totalRows {
		b.WriteString(fmt.Sprintf(`<div class="card-header">Data <span class="pill">%d of %d rows</span></div>`,
			len(rows), totalRows))
	} else {
		b.WriteString(fmt.Sprintf(`<div class="card-header">Data <span class="pill">%d rows</span></div>`, len(rows)))
	}

	b.WriteString(`<div class="data-wrapper"><table class="data-table"><thead><tr>`)
	b.WriteString(`<th class="row-num">#</th>`)
	for _, field := range schema.Fields {
		cls := ""
		if field.Key {
			cls = ` class="key-col"`
		}
		typeLabel := strings.ToLower(field.Type)
		if field.Length > 0 {
			typeLabel += fmt.Sprintf("(%d)", field.Length)
		}
		b.WriteString(fmt.Sprintf(`<th%s>%s<br><small>%s</small></th>`,
			cls, html.EscapeString(field.Name), html.EscapeString(typeLabel)))
	}
	b.WriteString(`</tr></thead><tbody>`)

	for i, vals := range rows {
		b.WriteString(`<tr>`)
		b.WriteString(fmt.Sprintf(`<td class="row-num">%d</td>`, i+1))
		for ci, val := range vals {
			if ci >= len(schema.Fields) {
				break
			}
			field := schema.Fields[ci]
			if val == "" {
				b.WriteString(`<td><span class="null-val">NULL</span></td>`)
				continue
			}
			switch strings.ToLower(field.Type) {
			case "integer", "decimal", "real":
				b.WriteString(`<td class="num-val">` + html.EscapeString(val) + `</td>`)
			case "boolean":
				cls := "bool-false"
				if val == "1" || strings.EqualFold(val, "true") {
					cls = "bool-true"
				}
				b.WriteString(`<td><span class="` + cls + `">` + html.EscapeString(val) + `</span></td>`)
			case "blob":
				b.WriteString(`<td><span class="null-val">&lt;binary&gt;</span></td>`)
			default:
				b.WriteString(`<td>` + html.EscapeString(val) + `</td>`)
			}
		}
		b.WriteString(`</tr>`)
	}

	b.WriteString(`</tbody></table></div>`)

	// Stats bar
	keyCount := 0
	for _, f := range schema.Fields {
		if f.Key {
			keyCount++
		}
	}
	b.WriteString(fmt.Sprintf(`<div class="stats-bar">
  <span><strong>%d</strong> rows shown</span>
  <span><strong>%d</strong> columns</span>
  <span><strong>%d</strong> primary key(s)</span>
</div>`, len(rows), len(schema.Fields), keyCount))

	b.WriteString(`</div>`) // data card
	b.WriteString(`<div class="footer"><a href="/">← back</a></div>`)
	b.WriteString(`</div></body></html>`)

	fmt.Fprint(w, b.String())
}

// ─────────────────────────────────────────────────────────────────────────────
// Shared HTML helpers
// ─────────────────────────────────────────────────────────────────────────────

func commonCSS() string {
	return `<style>
  * { box-sizing:border-box; margin:0; padding:0; }
  body { font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif; background:#0f1117; color:#e2e8f0; min-height:100vh; padding:24px; }
  .container { max-width:1600px; margin:0 auto; }
  .navbar {
    display:flex; align-items:center; gap:12px; margin-bottom:24px;
    padding-bottom:16px; border-bottom:1px solid #1e293b;
  }
  .nav-title { font-size:18px; font-weight:700; color:#f1f5f9; }
  .nav-sep   { color:#334155; }
  .nav-sub   { font-size:16px; color:#94a3b8; font-weight:500; }
  .nav-home  { color:#60a5fa; text-decoration:none; font-weight:700; }
  .nav-home:hover { color:#93c5fd; }
  .badge { display:inline-flex; align-items:center; gap:6px; padding:4px 10px; border-radius:20px; font-size:12px; font-weight:600; }
  .badge-reference { background:#1e3a5f; color:#60a5fa; }
  .badge-key       { background:#2d1b69; color:#a78bfa; }
  .header-card { background:linear-gradient(135deg,#1e293b 0%,#0f172a 100%); border:1px solid #334155; border-radius:12px; padding:24px 28px; margin-bottom:20px; }
  .header-top  { display:flex; align-items:center; gap:16px; flex-wrap:wrap; margin-bottom:16px; }
  .table-name  { font-size:26px; font-weight:700; color:#f1f5f9; }
  .meta-grid   { display:grid; grid-template-columns:repeat(auto-fill,minmax(200px,1fr)); gap:12px; }
  .meta-item   { display:flex; flex-direction:column; gap:2px; }
  .meta-label  { font-size:11px; font-weight:600; color:#64748b; text-transform:uppercase; letter-spacing:.05em; }
  .meta-value  { font-size:13px; color:#cbd5e1; font-family:monospace; word-break:break-all; }
  .card        { background:#1e293b; border:1px solid #334155; border-radius:12px; margin-bottom:20px; overflow:hidden; }
  .card-header { padding:14px 20px; border-bottom:1px solid #334155; font-size:14px; font-weight:600; color:#94a3b8; display:flex; align-items:center; gap:10px; background:#0f172a; }
  .pill        { background:#334155; color:#94a3b8; padding:2px 8px; border-radius:10px; font-size:11px; font-weight:600; }
  .stats-bar   { display:flex; gap:24px; flex-wrap:wrap; padding:12px 20px; background:#0f172a; border-top:1px solid #334155; font-size:12px; color:#64748b; }
  .stats-bar span { display:flex; align-items:center; gap:6px; }
  .stats-bar strong { color:#94a3b8; }
  .footer      { text-align:center; padding:20px; font-size:11px; color:#334155; }
  .footer a    { color:#475569; text-decoration:none; }
</style>`
}

func writeNavbar(b *strings.Builder, serverName, datasetName string) {
	b.WriteString(`<div class="navbar">`)
	b.WriteString(`<a class="nav-home" href="/">` + html.EscapeString(serverName) + `</a>`)
	if datasetName != "" {
		b.WriteString(`<span class="nav-sep">/</span>`)
		b.WriteString(`<span class="nav-sub">` + html.EscapeString(datasetName) + `</span>`)
	}
	b.WriteString(`</div>`)
}

func writeMetaItem(b *strings.Builder, label, value string) {
	b.WriteString(`<div class="meta-item">`)
	b.WriteString(`<span class="meta-label">` + html.EscapeString(label) + `</span>`)
	b.WriteString(`<span class="meta-value">` + html.EscapeString(value) + `</span>`)
	b.WriteString(`</div>`)
}
