package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// eventRoutesHarness is a router with only the event endpoints, the sinks the
// dispatcher would have been given, and two rows: one dead, one pending.
type eventRoutesHarness struct {
	router http.Handler
	db     *OrchestratorDB
	dead   string // id of the dead row
	pend   string // id of the pending row
}

func newEventRoutesHarness(t *testing.T) *eventRoutesHarness {
	t.Helper()
	db := newScheduleStatusDB(t)
	if err := db.AddOutbox([]OutboxEvent{
		{ID: "dead-1", JobID: "j1", Sink: "ops", Type: EventJobFinished, Payload: []byte(`{"secret":"x"}`), CreatedAt: time.Now()},
		{ID: "pend-1", JobID: "j2", Sink: "ops", Type: EventJobFinished, Payload: []byte(`{}`), CreatedAt: time.Now()},
	}); err != nil {
		t.Fatalf("AddOutbox: %v", err)
	}
	if err := db.markOutboxDead("dead-1", 10, "http_503"); err != nil {
		t.Fatalf("markOutboxDead: %v", err)
	}

	// A webhook URL with a token in it: the sink view must not show it.
	deps := routerDeps{db: db, eventSinks: []EventSinkDef{{
		Name:        "ops",
		Type:        "webhook",
		Types:       []string{EventJobFinished},
		MaxAttempts: 10,
		URL:         "https://hooks.example.com/tdtp?token=URLTOKEN-do-not-show",
	}}}
	r := chi.NewRouter()
	registerEventRoutes(r, deps)
	return &eventRoutesHarness{router: r, db: db, dead: "dead-1", pend: "pend-1"}
}

func (h *eventRoutesHarness) do(t *testing.T, method, target string, role Role) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	if role != "" {
		req = withPrincipal(req, &Principal{Name: "tester", Role: role})
	}
	rw := httptest.NewRecorder()
	h.router.ServeHTTP(rw, req)
	return rw
}

func TestEventRoutes_AdminOnly(t *testing.T) {
	h := newEventRoutesHarness(t)
	for _, role := range []Role{RoleConsumer, RoleActivator} {
		if rw := h.do(t, http.MethodGet, "/events/sinks", role); rw.Code != http.StatusForbidden {
			t.Fatalf("%s GET /events/sinks: %d, want 403", role, rw.Code)
		}
		if rw := h.do(t, http.MethodGet, "/events/outbox", role); rw.Code != http.StatusForbidden {
			t.Fatalf("%s GET /events/outbox: %d, want 403", role, rw.Code)
		}
		if rw := h.do(t, http.MethodPost, "/events/outbox/dead-1/retry", role); rw.Code != http.StatusForbidden {
			t.Fatalf("%s retry: %d, want 403", role, rw.Code)
		}
	}
	// The retry was refused for a consumer and an activator: the row is still dead.
	if rows, _ := h.db.listOutbox("dead", "", 10); len(rows) != 1 {
		t.Fatal("a refused retry changed the outbox")
	}
}

func TestEventRoutes_SinksShowTotalsAndNoURL(t *testing.T) {
	h := newEventRoutesHarness(t)
	rw := h.do(t, http.MethodGet, "/events/sinks", RoleAdmin)
	if rw.Code != http.StatusOK {
		t.Fatalf("status %d", rw.Code)
	}
	body := rw.Body.String()
	if strings.Contains(body, "URLTOKEN") || strings.Contains(body, "hooks.example.com") {
		t.Fatalf("sink view shows the webhook URL: %s", body)
	}

	var sinks []eventSinkView
	if err := json.Unmarshal(rw.Body.Bytes(), &sinks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sinks) != 1 || sinks[0].Name != "ops" || sinks[0].Dead != 1 || sinks[0].Pending != 1 {
		t.Fatalf("sinks = %+v, want ops with 1 dead and 1 pending", sinks)
	}
}

func TestEventRoutes_OutboxListOmitsPayloadAndFilters(t *testing.T) {
	h := newEventRoutesHarness(t)

	rw := h.do(t, http.MethodGet, "/events/outbox?status=dead", RoleAdmin)
	if rw.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	if strings.Contains(rw.Body.String(), "payload") {
		t.Fatalf("list carries the payload: %s", rw.Body.String())
	}
	var rows []outboxView
	if err := json.Unmarshal(rw.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != h.dead || rows[0].LastError == nil || *rows[0].LastError != "http_503" {
		t.Fatalf("dead rows = %+v", rows)
	}

	if rw := h.do(t, http.MethodGet, "/events/outbox", RoleAdmin); !strings.Contains(rw.Body.String(), h.pend) {
		t.Fatal("unfiltered list misses the pending row")
	}
	if rw := h.do(t, http.MethodGet, "/events/outbox?sink=nope", RoleAdmin); strings.TrimSpace(rw.Body.String()) != "[]" {
		t.Fatalf("sink filter with no match: %s", rw.Body.String())
	}
	for _, bad := range []string{"/events/outbox?status=bogus", "/events/outbox?limit=0", "/events/outbox?limit=501", "/events/outbox?limit=x"} {
		if rw := h.do(t, http.MethodGet, bad, RoleAdmin); rw.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d, want 400", bad, rw.Code)
		}
	}
}

func TestEventRoutes_RetryOnlyMovesDeadRows(t *testing.T) {
	h := newEventRoutesHarness(t)

	rw := h.do(t, http.MethodPost, "/events/outbox/"+h.dead+"/retry", RoleAdmin)
	if rw.Code != http.StatusOK {
		t.Fatalf("retry dead: %d %s", rw.Code, rw.Body.String())
	}
	rows, err := h.db.listOutbox("", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == h.dead && (r.Status != "pending" || r.Attempts != 0 || r.LastError != nil) {
			t.Fatalf("after retry: %+v, want pending, attempts reset, error cleared", r)
		}
	}

	// Already pending now: a second retry is a conflict, not a silent no-op.
	if rw := h.do(t, http.MethodPost, "/events/outbox/"+h.dead+"/retry", RoleAdmin); rw.Code != http.StatusConflict {
		t.Fatalf("retry of a pending row: %d, want 409", rw.Code)
	}
	if rw := h.do(t, http.MethodPost, "/events/outbox/"+h.pend+"/retry", RoleAdmin); rw.Code != http.StatusConflict {
		t.Fatalf("retry of pending row %s: %d, want 409", h.pend, rw.Code)
	}
	if rw := h.do(t, http.MethodPost, "/events/outbox/no-such-id/retry", RoleAdmin); rw.Code != http.StatusNotFound {
		t.Fatalf("retry of unknown id: %d, want 404", rw.Code)
	}
}

// A retried event is delivered by the dispatcher on its next pass.
func TestEventRoutes_RetriedEventIsDelivered(t *testing.T) {
	h := newEventRoutesHarness(t)
	if rw := h.do(t, http.MethodPost, "/events/outbox/"+h.dead+"/retry", RoleAdmin); rw.Code != http.StatusOK {
		t.Fatalf("retry: %d", rw.Code)
	}
	sink := &fakeSink{}
	d := newTestDispatcher(h.db, map[string]EventSink{"ops": sink}, 10)
	if n, err := d.RunOnce(t.Context()); err != nil || n != 2 {
		t.Fatalf("RunOnce = %d, %v; want both rows delivered", n, err)
	}
}
