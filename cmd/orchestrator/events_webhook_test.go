package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

const testHookSecret = "s3cret-value"

// hookSink builds a webhook sink for url with a short timeout, the secret
// coming from the environment as it would in production.
func hookSink(t *testing.T, url string, timeout time.Duration) *webhookSink {
	t.Helper()
	const envName = "TDTP_TEST_HOOK_SECRET"
	t.Setenv(envName, testHookSecret)
	sink, err := newWebhookSinkWithTimeout(EventSinkDef{
		Name:   "hook",
		Type:   "webhook",
		URL:    url,
		Secret: "env:" + envName,
		// httptest serves plain http on 127.0.0.1: the loopback opt-in.
		AllowInsecureHTTP: true,
	}, timeout)
	if err != nil {
		t.Fatalf("newWebhookSinkWithTimeout: %v", err)
	}
	return sink
}

// recorded is one request as the receiver saw it.
type recorded struct {
	method, contentType, eventID, timestamp, signature string
	body                                               []byte
}

type recorder struct {
	mu   sync.Mutex
	reqs []recorded
}

func (rec *recorder) handler(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.reqs = append(rec.reqs, recorded{
			method:      r.Method,
			contentType: r.Header.Get("Content-Type"),
			eventID:     r.Header.Get("X-TDTP-Event-ID"),
			timestamp:   r.Header.Get("X-TDTP-Timestamp"),
			signature:   r.Header.Get("X-TDTP-Signature"),
			body:        body,
		})
		rec.mu.Unlock()
		w.WriteHeader(status)
	}
}

func (rec *recorder) count() int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return len(rec.reqs)
}

func (rec *recorder) last(t *testing.T) recorded {
	t.Helper()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.reqs) == 0 {
		t.Fatal("receiver got no request")
	}
	return rec.reqs[len(rec.reqs)-1]
}

// classOf runs Send and returns the failure class and whether it is permanent;
// class is "" for success.
func classOf(t *testing.T, sink EventSink, ev OutboxEvent) (class string, permanent bool) {
	t.Helper()
	err := sink.Send(context.Background(), ev)
	if err == nil {
		return "", false
	}
	var se *SinkError
	if !errors.As(err, &se) {
		t.Fatalf("Send returned an unclassified error: %v", err)
	}
	return se.Class, se.Permanent
}

func TestWebhook_RequestIsSignedAndIdentified(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec.handler(http.StatusOK))
	defer srv.Close()

	sink := hookSink(t, srv.URL, webhookTimeout)
	payload := []byte(`{"event_id":"9f0c","type":"job.finished"}`)
	if err := sink.Send(context.Background(), OutboxEvent{ID: "9f0c", Payload: payload}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := rec.last(t)
	if got.method != http.MethodPost || got.contentType != "application/json" {
		t.Fatalf("method/content-type = %s %s", got.method, got.contentType)
	}
	if got.eventID != "9f0c" {
		t.Fatalf("X-TDTP-Event-ID = %q, want the row id", got.eventID)
	}
	if string(got.body) != string(payload) {
		t.Fatalf("body changed in transit: %s", got.body)
	}

	// Recompute the signature without calling signWebhook: the test checks the
	// protocol a receiver implements, not the function that produces it.
	mac := hmac.New(sha256.New, []byte(testHookSecret))
	mac.Write([]byte(got.timestamp + "."))
	mac.Write(got.body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if got.signature != want {
		t.Fatalf("signature = %s, want %s", got.signature, want)
	}
	if _, err := strconv.ParseInt(got.timestamp, 10, 64); err != nil {
		t.Fatalf("timestamp %q is not unix seconds", got.timestamp)
	}
}

func TestWebhook_ClassifiesReplies(t *testing.T) {
	cases := []struct {
		status    int
		class     string // "" = delivered
		permanent bool
	}{
		{http.StatusOK, "", false},
		{http.StatusNoContent, "", false},
		{http.StatusBadRequest, "http_400", true},
		{http.StatusUnauthorized, "http_401", true},
		{http.StatusNotFound, "http_404", true},
		{http.StatusRequestTimeout, "http_408", false},
		{http.StatusTooManyRequests, "http_429", false},
		{http.StatusInternalServerError, "http_500", false},
		{http.StatusServiceUnavailable, "http_503", false},
	}
	for _, c := range cases {
		t.Run(strconv.Itoa(c.status), func(t *testing.T) {
			srv := httptest.NewServer((&recorder{}).handler(c.status))
			defer srv.Close()
			class, perm := classOf(t, hookSink(t, srv.URL, webhookTimeout), OutboxEvent{ID: "e", Payload: []byte(`{}`)})
			if class != c.class || perm != c.permanent {
				t.Fatalf("status %d: class=%q permanent=%v, want class=%q permanent=%v",
					c.status, class, perm, c.class, c.permanent)
			}
		})
	}
}

// A redirect is not followed: the signature must not travel to the redirect
// target, and the 3xx is a refusal, not a delivery.
func TestWebhook_DoesNotFollowRedirect(t *testing.T) {
	target := &recorder{}
	tsrv := httptest.NewServer(target.handler(http.StatusOK))
	defer tsrv.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, tsrv.URL, http.StatusFound)
	}))
	defer redirect.Close()

	class, perm := classOf(t, hookSink(t, redirect.URL, webhookTimeout), OutboxEvent{ID: "e", Payload: []byte(`{}`)})
	if class != "http_302" || !perm {
		t.Fatalf("redirect: class=%q permanent=%v, want http_302 permanent", class, perm)
	}
	if n := target.count(); n != 0 {
		t.Fatalf("redirect target received %d requests; the signature must not follow a redirect", n)
	}
}

func TestWebhook_TimeoutIsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the body first: the server only notices a dropped connection, and
		// cancels r.Context(), once the request body has been consumed.
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done(): // the client gave up
		case <-time.After(3 * time.Second):
		}
	}))
	defer srv.Close()

	class, perm := classOf(t, hookSink(t, srv.URL, 50*time.Millisecond), OutboxEvent{ID: "e", Payload: []byte(`{}`)})
	if class != "timeout" || perm {
		t.Fatalf("timeout: class=%q permanent=%v, want timeout transient", class, perm)
	}
}

// A connection that is refused is transient, and the URL's query token must
// not reach the error text: that text is logged.
func TestWebhook_NetworkErrorIsTransientAndHidesURL(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // nothing listens here now

	const token = "TOKENVALUE-do-not-log"
	sink := hookSink(t, "http://"+addr+"/hook?token="+token, webhookTimeout)

	err = sink.Send(context.Background(), OutboxEvent{ID: "e", Payload: []byte(`{}`)})
	var se *SinkError
	if !errors.As(err, &se) || se.Class != "network" || se.Permanent {
		t.Fatalf("refused connection: %v, want network transient", err)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error text carries the URL token: %v", err)
	}
}

func TestWebhookDef_Validation(t *testing.T) {
	const env = "TDTP_TEST_HOOK_SECRET"
	cases := []struct {
		name string
		def  EventSinkDef
		want string // "" = valid
	}{
		{"https", EventSinkDef{URL: "https://crm.example.com/hook", Secret: "env:" + env}, ""},
		{"http non-local refused", EventSinkDef{URL: "http://crm.example.com/hook", Secret: "env:" + env, AllowInsecureHTTP: true}, "https"},
		{"http localhost needs opt-in", EventSinkDef{URL: "http://localhost:9000/hook", Secret: "env:" + env}, "https"},
		{"http localhost with opt-in", EventSinkDef{URL: "http://localhost:9000/hook", Secret: "env:" + env, AllowInsecureHTTP: true}, ""},
		{"http loopback ip with opt-in", EventSinkDef{URL: "http://127.0.0.1:9000/hook", Secret: "env:" + env, AllowInsecureHTTP: true}, ""},
		{"missing url", EventSinkDef{Secret: "env:" + env}, "url is required"},
		{"credentials in url refused", EventSinkDef{URL: "https://hook:PASSWORDVALUE@crm.example.com/", Secret: "env:" + env}, "credentials"},
		{"literal secret refused", EventSinkDef{URL: "https://crm.example.com/", Secret: "s3cret"}, "env:VARIABLE"},
		{"empty variable name", EventSinkDef{URL: "https://crm.example.com/", Secret: "env:"}, "env:VARIABLE"},
		{"no secret", EventSinkDef{URL: "https://crm.example.com/"}, "env:VARIABLE"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateWebhookDef(c.def)
			if c.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want substring %q", err, c.want)
			}
			// No error may echo the URL or the password it might contain.
			if strings.Contains(err.Error(), "PASSWORDVALUE") {
				t.Fatalf("error echoes credentials: %v", err)
			}
		})
	}
}

func TestPrepareSinks_WebhookFieldsOnlyForWebhook(t *testing.T) {
	defs := []EventSinkDef{{Name: "a", Type: "log", URL: "https://x.example.com/"}}
	if err := prepareSinks(defs); err == nil || !strings.Contains(err.Error(), "webhook sinks only") {
		t.Fatalf("log sink with a url accepted: %v", err)
	}
}

func TestLoadEvents_WebhookSecretMustBeSetAtStartup(t *testing.T) {
	const env = "TDTP_TEST_MISSING_SECRET"
	t.Setenv(env, "") // empty counts as not set

	path := filepath.Join(t.TempDir(), "events.yaml")
	body := "sinks:\n  - name: crm\n    type: webhook\n    url: https://crm.example.com/hook\n    secret: env:" + env + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadEvents(path)
	if err == nil || !strings.Contains(err.Error(), env) {
		t.Fatalf("LoadEvents without the secret: %v; want it to name %s", err, env)
	}

	t.Setenv(env, testHookSecret)
	defs, err := LoadEvents(path)
	if err != nil || len(defs) != 1 || defs[0].Type != "webhook" {
		t.Fatalf("LoadEvents with the secret: %v, %v", defs, err)
	}
}

// Through the dispatcher: a 503 is retried, the retry carries the same event_id
// and body under a fresh signature, and the metrics record both steps.
func TestDispatcher_WebhookRetriedThenDelivered(t *testing.T) {
	db := newScheduleStatusDB(t)
	payload := []byte(`{"event_id":"ev-1","type":"job.finished"}`)
	if err := db.AddOutbox([]OutboxEvent{{ID: "ev-1", JobID: "j", Sink: "hook", Type: EventJobFinished, Payload: payload, CreatedAt: time.Now()}}); err != nil {
		t.Fatalf("AddOutbox: %v", err)
	}

	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sentBefore := testutil.ToFloat64(eventsSentTotal.WithLabelValues("hook", EventJobFinished))
	failedBefore := testutil.ToFloat64(eventsFailedTotal.WithLabelValues("hook", "http_503"))

	d := newTestDispatcher(db, map[string]EventSink{"hook": hookSink(t, srv.URL, webhookTimeout)}, 5)
	if _, err := d.RunOnce(context.Background()); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	if rows := readOutbox(t, db); rows[0].Status != "pending" || rows[0].Attempts != 1 {
		t.Fatalf("after a 503: %+v, want pending with one attempt", rows[0])
	}
	if n, err := d.RunOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("pass 2 = %d, %v; want one delivery", n, err)
	}
	if rows := readOutbox(t, db); rows[0].Status != "sent" {
		t.Fatalf("after the retry: status=%s, want sent", rows[0].Status)
	}

	if got := testutil.ToFloat64(eventsSentTotal.WithLabelValues("hook", EventJobFinished)) - sentBefore; got != 1 {
		t.Fatalf("sent counter moved by %v, want 1", got)
	}
	if got := testutil.ToFloat64(eventsFailedTotal.WithLabelValues("hook", "http_503")) - failedBefore; got != 1 {
		t.Fatalf("failed counter moved by %v, want 1", got)
	}
}
