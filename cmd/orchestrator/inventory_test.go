package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rabbitmq/amqp091-go"

	s3store "github.com/ruslano69/tdtp-framework/pkg/storage/s3"
)

// scenarioFromFile loads a scenario through LoadScenario, so its orchestrator
// block (and with it the parameter definitions) is read as in production.
func scenarioFromFile(t *testing.T, name, body string) *Scenario {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadScenario(path)
	if err != nil {
		t.Fatalf("LoadScenario: %v", err)
	}
	return s
}

// inventoryHarness serves the inventory routes with a principal in each request.
type inventoryHarness struct{ router http.Handler }

func newInventoryHarness(t *testing.T, db *OrchestratorDB, svc *InventoryService, scenes map[string]*Scenario) *inventoryHarness {
	t.Helper()
	r := chi.NewRouter()
	registerInventoryRoutes(r, routerDeps{db: db, scenes: scenes, inventory: svc})
	return &inventoryHarness{router: r}
}

func (h *inventoryHarness) do(t *testing.T, method, target string, role Role) *httptest.ResponseRecorder {
	t.Helper()
	req := withPrincipal(httptest.NewRequest(method, target, nil), &Principal{Name: "tester", Role: role})
	rw := httptest.NewRecorder()
	h.router.ServeHTTP(rw, req)
	return rw
}

// ─── derivation ──────────────────────────────────────────────────────────────

const secretPassword = "PASSWORD-must-not-appear"

// A scenario that touches every kind of endpoint. Its keys must name each one
// and carry none of its credentials.
func TestDeriveTargets_KeysNameEndpointsAndCarryNoCredentials(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.ToSlash(filepath.Join(dir, "in.csv"))
	s := scenarioFromFile(t, "mixed", `orchestrator:
  name: mixed
sources:
  - name: orders
    type: postgres
    dsn: "postgres://tdtp:`+secretPassword+`@db.internal:5433/sales?sslmode=disable"
  - name: legacy
    type: mysql
    dsn: "tdtp:`+secretPassword+`@tcp(my.internal:3306)/hr?parseTime=true"
  - name: staff
    type: mssql
    dsn: "server=ms.internal,1433;user id=sa;password=`+secretPassword+`;database=DevDB"
  - name: lite
    type: sqlite
    dsn: "file:data/app.db?cache=shared"
  - name: feed
    type: csv
    dsn: "`+csvPath+`"
  - name: archive
    type: tdtp-s3
    dsn: "s3://archive-bucket/exports/2026.tdtp.xml"
    s3:
      endpoint: "http://s3.internal:8333"
      access_key: "k"
      secret_key: "`+secretPassword+`"
output:
  type: tdtp
  tdtp:
    destination: "s3://out-bucket/result.tdtp.xml"
    s3:
      endpoint: "http://s3.internal:8333"
      access_key: "k"
      secret_key: "`+secretPassword+`"
result_log:
  type: redis
  address: "redis.internal:6379"
  password: "`+secretPassword+`"
`)
	targets, err := deriveTargets(s, nil)
	if err != nil {
		t.Fatalf("deriveTargets: %v", err)
	}

	want := map[string]string{
		"postgres://db.internal:5433/sales":         "db",
		"mysql://my.internal:3306/hr":               "db",
		"mssql://ms.internal,1433/DevDB":            "db",
		"file:" + filepath.Clean("data/app.db"):     "file",
		"file:" + filepath.Clean(csvPath):           "file",
		"s3://archive-bucket/exports/2026.tdtp.xml": "s3",
		"s3://out-bucket":                           "s3",
		"redis://redis.internal:6379/0":             "redis",
	}
	got := map[string]string{}
	var keys []string
	for _, tg := range targets {
		got[tg.Key] = tg.Kind
		keys = append(keys, tg.Key)
	}
	for key, kind := range want {
		if got[key] != kind {
			t.Errorf("target %q: kind %q, want %q; have %v", key, got[key], kind, keys)
		}
	}
	for _, key := range keys {
		if strings.Contains(key, secretPassword) || strings.Contains(key, "tdtp:") || strings.Contains(key, "@") {
			t.Errorf("target key carries credentials: %q", key)
		}
	}
}

func TestDBEndpointKey(t *testing.T) {
	cases := []struct {
		typ, dsn, want string
	}{
		{"postgres", "postgresql://u:p@h.internal:5432/db?sslmode=disable", "postgres://h.internal:5432/db"},
		{"oracle", "oracle://u:p@ora.internal:1521/XEPDB1", "oracle://ora.internal:1521/XEPDB1"},
		{"mysql", "u:p@tcp(m.internal:3306)/db?parseTime=true", "mysql://m.internal:3306/db"},
		{"mssql", "server=ms.internal;user id=u;password=p;database=DevDB", "mssql://ms.internal/DevDB"},
	}
	for _, c := range cases {
		if got := dbEndpointKey(c.typ, c.dsn); got != c.want {
			t.Errorf("dbEndpointKey(%s) = %q, want %q", c.typ, got, c.want)
		}
	}
	// A DSN that cannot be read gets an opaque key: a hash prefix, no fragment.
	key := dbEndpointKey("postgres", "garbage-with-password-p")
	if strings.Contains(key, "password") || !strings.HasPrefix(key, "db:postgres:") {
		t.Fatalf("unreadable DSN key = %q; want an opaque db: key", key)
	}
}

// A parameter with no value leaves the scenario unresolved, classed "param".
// A template that does not render is "template". Neither is an endpoint failure.
func TestDeriveTargets_UnresolvedClasses(t *testing.T) {
	required := scenarioFromFile(t, "needs-period", `orchestrator:
  name: needs-period
  params:
    - name: period
      required: true
sources:
  - name: s
    type: csv
    dsn: "/tmp/{{.period}}.csv"
`)
	_, err := deriveTargets(required, nil)
	var ue *unresolvedError
	if !errors.As(err, &ue) || ue.class != "param" {
		t.Fatalf("missing required param: %v; want unresolved/param", err)
	}

	broken := scenarioFromFile(t, "broken", "orchestrator:\n  name: broken\nsources:\n  - name: s\n    type: csv\n    dsn: \"{{.undeclared}}\"\n")
	_, err = deriveTargets(broken, nil)
	if !errors.As(err, &ue) || ue.class != "template" {
		t.Fatalf("undeclared key: %v; want unresolved/template", err)
	}
}

// ─── classification ──────────────────────────────────────────────────────────

func TestClassifyProbeErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"timeout", errProbeTimeout, "timeout"},
		{"deadline", context.DeadlineExceeded, "timeout"},
		{"no queue", errNoQueue, "config"},
		{"not a dir", errNotDir, "config"},
		{"s3 404", &s3store.Error{StatusCode: 404}, "not_found"},
		{"s3 403", &s3store.Error{StatusCode: 403}, "auth"},
		{"s3 500", &s3store.Error{StatusCode: 500}, "other"},
		{"missing file", &fs.PathError{Op: "stat", Path: "x", Err: fs.ErrNotExist}, "not_found"},
		{"no permission", &fs.PathError{Op: "stat", Path: "x", Err: fs.ErrPermission}, "permission"},
		{"amqp refused", &amqp091.Error{Code: amqp091.AccessRefused}, "auth"},
		{"amqp no queue", &amqp091.Error{Code: amqp091.NotFound}, "not_found"},
		{"postgres auth", errors.New("failed to connect to postgres: FATAL: password authentication failed for user \"tdtp\""), "auth"},
		{"oracle auth", errors.New("ORA-01017: invalid username/password; logon denied"), "auth"},
		{"sql server db", errors.New("cannot open database \"DevDB\" requested by the login"), "not_found"},
		{"refused", errors.New("dial tcp 127.0.0.1:5432: connect: connection refused"), "network"},
		{"no host", errors.New("lookup db.nowhere: no such host"), "network"},
		{"unknown", errors.New("something odd happened"), "other"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyProbeErr(c.err); got != c.want {
				t.Fatalf("classifyProbeErr(%v) = %q, want %q", c.err, got, c.want)
			}
		})
	}
	// The class is a short word, never the text it came from.
	secret := errors.New("connection refused to host secret-host.internal")
	if got := classifyProbeErr(secret); strings.Contains(got, "secret-host") {
		t.Fatalf("class carries error text: %q", got)
	}
}

// A probe that never answers is abandoned: the caller is not held hostage by a
// driver that ignores its context.
func TestRunProbe_AbandonsAProbeThatNeverAnswers(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	parent, cancel := context.WithCancel(context.Background())
	cancel() // the run is already over; the probe must not be waited for
	err := runProbe(parent, func(context.Context) error {
		<-block
		return nil
	})
	if !errors.Is(err, errProbeTimeout) {
		t.Fatalf("runProbe = %v, want errProbeTimeout", err)
	}
}

// ─── the run ─────────────────────────────────────────────────────────────────

func inventoryScenes(t *testing.T, present string) map[string]*Scenario {
	t.Helper()
	dir := filepath.ToSlash(filepath.Dir(present))
	return map[string]*Scenario{
		"reads-present": scenarioFromFile(t, "reads-present",
			"orchestrator:\n  name: reads-present\nsources:\n  - name: s\n    type: csv\n    dsn: \""+filepath.ToSlash(present)+"\"\n"),
		"reads-missing": scenarioFromFile(t, "reads-missing",
			"orchestrator:\n  name: reads-missing\nsources:\n  - name: s\n    type: csv\n    dsn: \""+filepath.ToSlash(filepath.Join(dir, "gone.csv"))+"\"\n"),
		"needs-param": scenarioFromFile(t, "needs-param",
			"orchestrator:\n  name: needs-param\n  params:\n    - name: day\n      required: true\nsources:\n  - name: s\n    type: csv\n    dsn: \"/x/{{.day}}.csv\"\n"),
	}
}

func TestInventory_RunRecordsStatusPerScenario(t *testing.T) {
	db := newScheduleStatusDB(t)
	dir := t.TempDir()
	present := filepath.Join(dir, "here.csv")
	if err := os.WriteFile(present, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := NewInventoryService(db, inventoryScenes(t, present))

	id, ran, err := svc.Run(context.Background(), true)
	if err != nil || !ran {
		t.Fatalf("Run = %q, %v, %v; want a run", id, ran, err)
	}
	rows, err := db.inventoryRows(id)
	if err != nil {
		t.Fatal(err)
	}

	status := func(name string) string {
		st, found := scenarioStatus(rows, name)
		if !found {
			t.Fatalf("scenario %s not in the run", name)
		}
		return st
	}
	if got := status("reads-present"); got != "ok" {
		t.Errorf("reads-present = %s, want ok", got)
	}
	if got := status("reads-missing"); got != "degraded" {
		t.Errorf("reads-missing = %s, want degraded", got)
	}
	if got := status("needs-param"); got != "unresolved" {
		t.Errorf("needs-param = %s, want unresolved", got)
	}
	for _, r := range rows {
		if r.Key == "file:"+filepath.Join(dir, "gone.csv") && (r.Status != "degraded" || r.Class != "not_found") {
			t.Errorf("missing file row = %+v; want degraded/not_found", r)
		}
	}
}

// One run at a time, and no repeat inside the TTL unless forced.
func TestInventory_BeginRefusesSecondRunAndHonoursTTL(t *testing.T) {
	db := newScheduleStatusDB(t)
	svc := NewInventoryService(db, map[string]*Scenario{})

	first, started, err := svc.Begin(true)
	if err != nil || !started {
		t.Fatalf("first Begin = %q, %v, %v", first, started, err)
	}
	again, started, _ := svc.Begin(true)
	if started || again != first {
		t.Fatalf("second Begin while in flight = %q, started=%v; want the in-flight id", again, started)
	}
	if err := svc.Execute(context.Background(), first); err != nil {
		t.Fatal(err)
	}

	// Finished a moment ago: within the TTL a plain request reuses it.
	fresh, started, _ := svc.Begin(false)
	if started || fresh != first {
		t.Fatalf("Begin inside TTL = %q, started=%v; want the last run", fresh, started)
	}
	// Forced: a new run.
	forced, started, _ := svc.Begin(true)
	if !started || forced == first {
		t.Fatalf("forced Begin = %q, started=%v; want a new run", forced, started)
	}
	_ = svc.Execute(context.Background(), forced)
}

// The once-mode exit code: 1 when an endpoint is degraded, 0 otherwise. The
// table names the endpoint and shows its class.
func TestRunInventoryOnce_ExitCodeAndTable(t *testing.T) {
	db := newScheduleStatusDB(t)
	dir := t.TempDir()
	svc := NewInventoryService(db, map[string]*Scenario{
		"reads-missing": scenarioFromFile(t, "reads-missing",
			"orchestrator:\n  name: reads-missing\nsources:\n  - name: s\n    type: csv\n    dsn: \""+filepath.ToSlash(filepath.Join(dir, "gone.csv"))+"\"\n"),
	})
	var out bytes.Buffer
	if code := runInventoryOnce(context.Background(), svc, db, &out); code != 1 {
		t.Fatalf("exit code = %d, want 1 for a degraded endpoint", code)
	}
	if !strings.Contains(out.String(), "degraded") || !strings.Contains(out.String(), "not_found") {
		t.Fatalf("table does not show the degraded endpoint:\n%s", out.String())
	}

	okDir := t.TempDir()
	okFile := filepath.Join(okDir, "here.csv")
	if err := os.WriteFile(okFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	db2 := newScheduleStatusDB(t)
	svc2 := NewInventoryService(db2, map[string]*Scenario{
		"fine": scenarioFromFile(t, "fine", "orchestrator:\n  name: fine\nsources:\n  - name: s\n    type: csv\n    dsn: \""+filepath.ToSlash(okFile)+"\"\n"),
	})
	var out2 bytes.Buffer
	if code := runInventoryOnce(context.Background(), svc2, db2, &out2); code != 0 {
		t.Fatalf("exit code = %d, want 0 when everything answers:\n%s", code, out2.String())
	}
}

// The admin view carries endpoints; the consumer's aggregate carries none. This
// is checked on the JSON a client would actually get.
func TestInventoryRoutes_ConsumerSeesNoEndpoints(t *testing.T) {
	db := newScheduleStatusDB(t)
	dir := t.TempDir()
	scenes := map[string]*Scenario{
		"reads-missing": scenarioFromFile(t, "reads-missing",
			"orchestrator:\n  name: reads-missing\nsources:\n  - name: s\n    type: csv\n    dsn: \""+filepath.ToSlash(filepath.Join(dir, "secret-folder", "gone.csv"))+"\"\n"),
	}
	svc := NewInventoryService(db, scenes)
	if _, _, err := svc.Run(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	h := newInventoryHarness(t, db, svc, scenes)

	consumer := h.do(t, http.MethodGet, "/scenarios/reads-missing/inventory", RoleConsumer)
	if consumer.Code != http.StatusOK {
		t.Fatalf("consumer aggregate: %d", consumer.Code)
	}
	if strings.Contains(consumer.Body.String(), "secret-folder") || strings.Contains(consumer.Body.String(), "gone.csv") {
		t.Fatalf("consumer sees an endpoint: %s", consumer.Body.String())
	}
	var agg map[string]any
	if err := json.Unmarshal(consumer.Body.Bytes(), &agg); err != nil || agg["status"] != "degraded" {
		t.Fatalf("consumer aggregate = %v (%v); want status degraded", agg, err)
	}

	if rw := h.do(t, http.MethodGet, "/inventory", RoleConsumer); rw.Code != http.StatusForbidden {
		t.Fatalf("consumer on /inventory: %d, want 403", rw.Code)
	}
	admin := h.do(t, http.MethodGet, "/inventory", RoleAdmin)
	if admin.Code != http.StatusOK || !strings.Contains(admin.Body.String(), "secret-folder") {
		t.Fatalf("admin view: %d %s", admin.Code, admin.Body.String())
	}
	if rw := h.do(t, http.MethodGet, "/scenarios/no-such/inventory", RoleConsumer); rw.Code != http.StatusNotFound {
		t.Fatalf("unknown scenario: %d, want 404", rw.Code)
	}
}

// ─── targets that need a live service are covered in inventory_live_test.go ──

func TestRabbitMQTarget_KeyHasNoCredentials(t *testing.T) {
	tg := rabbitMQTarget(invRabbitMQ{Host: "mq.internal", Port: 0, User: "ops", Password: secretPassword, Queue: "q"})
	if tg.Key != "rabbitmq://mq.internal:5672/" {
		t.Fatalf("key = %q", tg.Key)
	}
	if strings.Contains(tg.Key, secretPassword) || strings.Contains(tg.Key, "ops") {
		t.Fatalf("key carries credentials: %q", tg.Key)
	}
}

func TestProbeRabbitMQ_NeedsAQueue(t *testing.T) {
	if err := probeRabbitMQ(invRabbitMQ{Host: "h"}, 5672, "/"); !errors.Is(err, errNoQueue) {
		t.Fatalf("probe without a queue: %v, want errNoQueue", err)
	}
}

func TestInventoryTable_ShowsEndpointAndStatus(t *testing.T) {
	var b bytes.Buffer
	WriteInventoryTable(&b, time.Unix(0, 0), []invRow{{Key: "file:/x", Kind: "file", Status: "ok", Scenarios: []string{"a"}}})
	if !strings.Contains(b.String(), "file:/x") || !strings.Contains(b.String(), "STATUS") {
		t.Fatalf("table = %s", b.String())
	}
}

// Scenarios that reach one object with the same credentials share one row. With
// different credentials they are probed separately: two rows, and the second
// key carries a suffix so a reader can tell them apart.
func TestInventory_SharedEndpointDeduplicatesByCredentials(t *testing.T) {
	db := newScheduleStatusDB(t)
	source := func(accessKey string) string {
		return "sources:\n  - name: s\n    type: tdtp-s3\n    dsn: \"s3://shared/x.tdtp.xml\"\n" +
			"    s3:\n      endpoint: \"http://127.0.0.1:1\"\n      access_key: \"" + accessKey + "\"\n      secret_key: \"k\"\n"
	}
	scenes := map[string]*Scenario{
		"a": scenarioFromFile(t, "a", "orchestrator:\n  name: a\n"+source("ka")),
		"b": scenarioFromFile(t, "b", "orchestrator:\n  name: b\n"+source("ka")),
		"c": scenarioFromFile(t, "c", "orchestrator:\n  name: c\n"+source("kc")),
	}
	svc := NewInventoryService(db, scenes)
	id, _, err := svc.Run(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.inventoryRows(id)
	if err != nil {
		t.Fatal(err)
	}
	var shared, separate *invRow
	for i := range rows {
		if rows[i].Kind != "s3" {
			continue
		}
		switch {
		case len(rows[i].Scenarios) == 2:
			shared = &rows[i]
		case len(rows[i].Scenarios) == 1 && rows[i].Scenarios[0] == "c":
			separate = &rows[i]
		}
	}
	if shared == nil || separate == nil {
		t.Fatalf("want one row for a+b and one for c, got %+v", rows)
	}
	if shared.Key != "s3://shared/x.tdtp.xml" {
		t.Errorf("shared row key = %q, want the plain endpoint key", shared.Key)
	}
	if separate.Key != "s3://shared/x.tdtp.xml #2" {
		t.Errorf("separate row key = %q, want the #2 suffix", separate.Key)
	}
}
