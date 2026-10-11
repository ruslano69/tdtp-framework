package main

// events_webhook.go — the webhook sink: a signed HTTP POST per event.
//
// A receiver can check that a request came from this orchestrator and is not a
// replay:
//
//	X-TDTP-Event-ID   the outbox row's event_id — the dedupe key
//	X-TDTP-Timestamp  unix seconds when this attempt was signed
//	X-TDTP-Signature  sha256=HMAC-SHA256(secret, timestamp + "." + body), hex
//
// The timestamp is inside the signed string, so a receiver that rejects old
// timestamps also rejects a captured request replayed later. A retry is signed
// again with a fresh timestamp: the body and event_id stay the same.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	webhookTimeout  = 5 * time.Second
	webhookDrain    = 4096 // read at most this much of a reply, so the connection can be reused
	secretEnvPrefix = "env:"
)

// validateWebhookDef checks the fields of a webhook sink. It never echoes the
// URL in an error: a URL may carry a token in its query string or credentials
// in its userinfo, and an error message ends up in a log.
func validateWebhookDef(def EventSinkDef) error {
	if def.URL == "" {
		return errors.New("url is required for a webhook sink")
	}
	u, err := url.Parse(def.URL)
	if err != nil {
		return errors.New("url is not a valid URL")
	}
	if u.User != nil {
		return errors.New("url must not carry credentials; put the secret in the secret field")
	}
	if u.Host == "" {
		return errors.New("url has no host")
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && def.AllowInsecureHTTP && isLoopbackHost(u.Hostname()):
	default:
		return errors.New("url must be https; plain http is accepted only for localhost with allow_insecure_http")
	}
	if !strings.HasPrefix(def.Secret, secretEnvPrefix) || len(def.Secret) == len(secretEnvPrefix) {
		return errors.New("secret must be env:VARIABLE — a literal secret is refused so it cannot end up in a file")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// sinkSecret reads the secret named by the definition. Only the variable's
// name is ever mentioned in an error, never its value.
func sinkSecret(def EventSinkDef) ([]byte, error) {
	name := strings.TrimPrefix(def.Secret, secretEnvPrefix)
	val := os.Getenv(name)
	if val == "" {
		return nil, fmt.Errorf("sink %q: environment variable %s is not set", def.Name, name)
	}
	return []byte(val), nil
}

type webhookSink struct {
	url    string
	secret []byte
	client *http.Client
	now    func() time.Time
}

func newWebhookSink(def EventSinkDef) (*webhookSink, error) {
	return newWebhookSinkWithTimeout(def, webhookTimeout)
}

func newWebhookSinkWithTimeout(def EventSinkDef, timeout time.Duration) (*webhookSink, error) {
	if err := validateWebhookDef(def); err != nil {
		return nil, fmt.Errorf("sink %q: %w", def.Name, err)
	}
	secret, err := sinkSecret(def)
	if err != nil {
		return nil, err
	}
	return &webhookSink{
		url:    def.URL,
		secret: secret,
		client: &http.Client{
			Timeout: timeout,
			// A redirect is not followed. The signature would travel to a host
			// the administrator never named; and a 3xx is reported as a refusal
			// rather than silently taken as the answer of some other server.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		now: time.Now,
	}, nil
}

// Send posts one event. It returns nil only for a 2xx reply.
func (s *webhookSink) Send(ctx context.Context, ev OutboxEvent) error {
	ts := strconv.FormatInt(s.now().Unix(), 10)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(ev.Payload))
	if err != nil {
		// The URL was parsed at startup; failing here means something changed
		// under us, and retrying cannot fix it.
		return &SinkError{Class: "other", Permanent: true, Err: errors.New("cannot build request")}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TDTP-Event-ID", ev.ID)
	req.Header.Set("X-TDTP-Timestamp", ts)
	req.Header.Set("X-TDTP-Signature", signWebhook(s.secret, ts, ev.Payload))

	resp, err := s.client.Do(req)
	if err != nil {
		return classifyTransportErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, webhookDrain))
	return classifyWebhookStatus(resp.StatusCode)
}

// signWebhook is the header value a receiver recomputes and compares.
func signWebhook(secret []byte, ts string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(ts))
	mac.Write([]byte{'.'})
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// classifyWebhookStatus decides whether a reply means delivered, retry, or give
// up. 408 and 429 are the receiver asking for the same request later; other
// 4xx refuse this request for good; 3xx is a refusal because redirects are not
// followed.
func classifyWebhookStatus(code int) error {
	if code >= 200 && code < 300 {
		return nil
	}
	class := "http_" + strconv.Itoa(code)
	err := fmt.Errorf("receiver answered %d", code)
	if code == 408 || code == 429 || code >= 500 {
		return &SinkError{Class: class, Err: err}
	}
	return &SinkError{Class: class, Permanent: true, Err: err}
}

// classifyTransportErr separates a deadline from a connection failure. Both
// are transient; the class tells an operator which one it was.
func classifyTransportErr(err error) error {
	// http.Client wraps every failure in a *url.Error, whose text starts with
	// the full URL. The URL can carry a token in its query, and this error is
	// logged, so the wrapper is taken off. The cause underneath names no URL.
	var ue *url.Error
	cause := err
	if errors.As(err, &ue) {
		cause = ue.Err
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return &SinkError{Class: "timeout", Err: cause}
	}
	return &SinkError{Class: "network", Err: cause}
}
