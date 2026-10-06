// s3_test.go — stdlib S3 driver tests, no live server needed.
//
// SigV4 golden vectors come from an independent Python implementation
// (sigv4_reference.py); the fake-S3 round trip additionally re-verifies
// every request signature on the server side.

package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/storage"
)

const (
	testAK     = "AKIDEXAMPLE"
	testSK     = "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"
	testRegion = "us-east-1"
)

var testTime = time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)

// ─── golden SigV4 vectors ───────────────────────────────────────────────────

func TestGoldenVectors(t *testing.T) {
	emptyHash := sha256Hex(nil)
	cases := []struct {
		name        string
		method      string
		uri         string
		query       string
		host        string
		payloadHash string
		want        string
	}{
		{
			name:        "GET object, no query",
			method:      "GET",
			uri:         "/bucket/test-key",
			query:       "",
			host:        "s3.us-east-1.amazonaws.com",
			payloadHash: emptyHash,
			want: "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20130524/us-east-1/s3/aws4_request, " +
				"SignedHeaders=host;x-amz-content-sha256;x-amz-date, " +
				"Signature=9873321a801bb930685de43df1778f6f1b632e51c73f66633b92ccf4c2ef51c1",
		},
		{
			name:   "GET bucket list, sorted query",
			method: "GET",
			uri:    "/bucket/",
			query: canonicalQuery([][2]string{
				{"prefix", "a/b"}, {"list-type", "2"}, {"max-keys", "1000"},
			}),
			host:        "s3.us-east-1.amazonaws.com",
			payloadHash: emptyHash,
			want: "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20130524/us-east-1/s3/aws4_request, " +
				"SignedHeaders=host;x-amz-content-sha256;x-amz-date, " +
				"Signature=2a7ae4b169734cb021b00024070e72ebf6e19954a63e6d2174a81433e4539275",
		},
		{
			name:        "PUT unicode key with space",
			method:      "PUT",
			uri:         canonicalURI("bucket", "папка/файл с пробелом.xml"),
			query:       "",
			host:        "localhost:9000",
			payloadHash: sha256Hex([]byte("hello")),
			want: "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20130524/us-east-1/s3/aws4_request, " +
				"SignedHeaders=host;x-amz-content-sha256;x-amz-date, " +
				"Signature=9923adad1b77dccc4def8373737b3de475b3f048d540526ccb96a3a1e660ee6b",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := signV4(c.method, c.uri, c.query, c.host, c.payloadHash, nil, testAK, testSK, testRegion, testTime)
			if got != c.want {
				t.Errorf("mismatch:\n got: %s\nwant: %s", got, c.want)
			}
		})
	}
}

func TestGoldenVectorWithMeta(t *testing.T) {
	// PUT with user metadata: x-amz-meta-* joins the signed headers.
	// Expected value produced by the independent Python reference
	// (sigv4_reference.py, "PUT with metadata" case).
	uri := canonicalURI("bucket", "k.xml")
	extra := map[string]string{"x-amz-meta-tdtp-table": "users"}
	got := signV4("PUT", uri, "", "s3.us-east-1.amazonaws.com",
		sha256Hex([]byte("hello")), extra, testAK, testSK, testRegion, testTime)
	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;x-amz-content-sha256;x-amz-date;x-amz-meta-tdtp-table, " +
		"Signature=cb334b2576f97ad0e7cb43c2d6fea5c49e48354cb2a73ed8a84973efc1473b59"
	if got != want {
		t.Errorf("mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestEscapeSigV4(t *testing.T) {
	cases := []struct{ in, wantPath, wantQuery string }{
		{"abc-_.~123", "abc-_.~123", "abc-_.~123"},
		{"a b", "a%20b", "a%20b"},
		{"a/b", "a/b", "a%2Fb"},
		{"a+b", "a%2Bb", "a%2Bb"},
		{"ключ", "%D0%BA%D0%BB%D1%8E%D1%87", "%D0%BA%D0%BB%D1%8E%D1%87"},
		{"100%", "100%25", "100%25"},
	}
	for _, c := range cases {
		if got := escapeSigV4(c.in, true); got != c.wantPath {
			t.Errorf("path escape(%q) = %q, want %q", c.in, got, c.wantPath)
		}
		if got := escapeSigV4(c.in, false); got != c.wantQuery {
			t.Errorf("query escape(%q) = %q, want %q", c.in, got, c.wantQuery)
		}
	}
}

// ─── fake S3 ────────────────────────────────────────────────────────────────

type fakeObject struct {
	body []byte
	meta map[string]string // lowercased, without x-amz-meta- prefix
}

type fakeS3 struct {
	t       *testing.T
	mu      sync.Mutex
	objs    map[string]*fakeObject
	badAuth int
}

func (f *fakeS3) verifyAuth(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	amzDate := r.Header.Get("X-Amz-Date")
	claimedHash := r.Header.Get("X-Amz-Content-Sha256")
	if auth == "" || amzDate == "" || claimedHash == "" {
		return false
	}
	t, err := time.Parse("20060102T150405Z", amzDate)
	if err != nil {
		return false
	}
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	if sha256Hex(body) != claimedHash {
		return false
	}
	extra := map[string]string{}
	for k, vv := range r.Header {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "x-amz-") &&
			lk != "x-amz-date" && lk != "x-amz-content-sha256" &&
			lk != "authorization" && len(vv) > 0 {
			extra[lk] = vv[0]
		}
	}
	want := signV4(r.Method, r.URL.EscapedPath(), r.URL.RawQuery, r.Host,
		claimedHash, extra, testAK, testSK, testRegion, t)
	return want == auth
}

func (f *fakeS3) writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, msg)
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !f.verifyAuth(r) {
		f.mu.Lock()
		f.badAuth++
		f.mu.Unlock()
		f.writeErr(w, http.StatusForbidden, "SignatureDoesNotMatch", "bad signature")
		return
	}
	// Path-style: /<bucket>/<key...>. Tests use bucket "b".
	path := strings.TrimPrefix(r.URL.Path, "/b")
	key := strings.TrimPrefix(path, "/")
	isBucketOp := path == "" || path == "/"

	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodPut && isBucketOp:
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		meta := map[string]string{}
		for k, vv := range r.Header {
			if strings.HasPrefix(k, "X-Amz-Meta-") {
				meta[strings.ToLower(strings.TrimPrefix(k, "X-Amz-Meta-"))] = vv[0]
			}
		}
		f.objs[key] = &fakeObject{body: body, meta: meta}
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && isBucketOp:
		f.list(w, r)
	case r.Method == http.MethodGet:
		o, ok := f.objs[key]
		if !ok {
			f.writeErr(w, http.StatusNotFound, "NoSuchKey", "not found")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(o.body)
	case r.Method == http.MethodHead:
		o, ok := f.objs[key]
		if !ok {
			// Real S3 HeadObject errors have no XML body — status only.
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(o.body)))
		w.Header().Set("Last-Modified", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).Format(http.TimeFormat))
		for k, v := range o.meta {
			w.Header().Set("X-Amz-Meta-"+k, v)
		}
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodDelete:
		delete(f.objs, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		f.writeErr(w, http.StatusBadRequest, "BadRequest", "unsupported")
	}
}

// list serves ListObjectsV2 with page size 2 to exercise pagination.
func (f *fakeS3) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("list-type") != "2" {
		f.writeErr(w, http.StatusBadRequest, "BadRequest", "need list-type=2")
		return
	}
	prefix := q.Get("prefix")
	var keys []string
	for k := range f.objs {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	start := 0
	if tok := q.Get("continuation-token"); tok != "" {
		_, _ = fmt.Sscanf(tok, "next-%d", &start)
	}
	const pageSize = 2
	end := start + pageSize
	truncated := false
	var next string
	if end < len(keys) {
		truncated = true
		next = fmt.Sprintf("next-%d", end)
	} else {
		end = len(keys)
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString(`<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
	b.WriteString(fmt.Sprintf(`<IsTruncated>%t</IsTruncated>`, truncated))
	if truncated {
		b.WriteString(`<NextContinuationToken>` + next + `</NextContinuationToken>`)
	}
	for _, k := range keys[start:end] {
		o := f.objs[k]
		fmt.Fprintf(&b, `<Contents><Key>%s</Key><Size>%d</Size><LastModified>2026-01-02T03:04:05.000Z</LastModified></Contents>`,
			k, len(o.body))
	}
	b.WriteString(`</ListBucketResult>`)
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, b.String())
}

func newTestDriver(t *testing.T, f *fakeS3) (context.Context, *Driver) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	d, err := newDriver(storage.Config{Type: "s3", S3: storage.S3Config{
		Endpoint:  srv.URL,
		Region:    testRegion,
		Bucket:    "b",
		AccessKey: testAK,
		SecretKey: testSK,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return context.Background(), d
}

func TestRoundTrip(t *testing.T) {
	f := &fakeS3{t: t, objs: map[string]*fakeObject{}}
	ctx, d := newTestDriver(t, f)

	// Put three objects (one with a unicode key) + metadata.
	puts := map[string]struct {
		body string
		meta map[string]string
	}{
		"pre/a.xml":          {"<a/>", map[string]string{"table": "users", "rows": "1"}},
		"pre/b.xml":          {"<b/>", nil},
		"pre/папка/файл.xml": {"<c/>", map[string]string{"table": "orders"}},
	}
	for k, p := range puts {
		if err := d.Put(ctx, k, strings.NewReader(p.body), p.meta); err != nil {
			t.Fatalf("Put %s: %v", k, err)
		}
	}

	// Get round-trips bytes.
	rc, err := d.Get(ctx, "pre/a.xml")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "<a/>" {
		t.Errorf("Get = %q, want %q", got, "<a/>")
	}

	// Unicode key round-trips.
	rc, err = d.Get(ctx, "pre/папка/файл.xml")
	if err != nil {
		t.Fatalf("Get unicode: %v", err)
	}
	got, _ = io.ReadAll(rc)
	rc.Close()
	if string(got) != "<c/>" {
		t.Errorf("Get unicode = %q", got)
	}

	// Stat: size + metadata.
	info, err := d.Stat(ctx, "pre/a.xml")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size != 4 {
		t.Errorf("Stat.Size = %d, want 4", info.Size)
	}
	if info.Metadata["tdtp-table"] != "users" || info.Metadata["tdtp-rows"] != "1" {
		t.Errorf("Stat.Metadata = %v", info.Metadata)
	}

	// List paginates (3 objects, page size 2).
	objs, err := d.List(ctx, "pre/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(objs) != 3 {
		t.Fatalf("List = %d objects, want 3", len(objs))
	}

	// Delete + Stat-missing error carries the S3 code.
	if err := d.Delete(ctx, "pre/b.xml"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = d.Stat(ctx, "pre/b.xml")
	s3err, ok := err.(*Error)
	if !ok {
		t.Fatalf("Stat missing: err type %T, want *Error", err)
	}
	if s3err.StatusCode != 404 || s3err.Code != "NotFound" {
		t.Errorf("Stat missing: %+v", s3err)
	}

	// Every request the fake saw carried a valid signature.
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.badAuth != 0 {
		t.Errorf("fake S3 rejected %d requests for bad signatures", f.badAuth)
	}
}

func TestEnsureBucket(t *testing.T) {
	f := &fakeS3{t: t, objs: map[string]*fakeObject{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	cfg := storage.Config{Type: "s3", S3: storage.S3Config{
		Endpoint:  srv.URL,
		Region:    testRegion,
		Bucket:    "b",
		AccessKey: testAK,
		SecretKey: testSK,
	}}
	if err := EnsureBucket(context.Background(), cfg); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.badAuth != 0 {
		t.Errorf("fake S3 rejected %d requests for bad signatures", f.badAuth)
	}
}

func TestNewDriverEndpoint(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		disable  bool
		region   string
		want     string
	}{
		{"scheme kept", "http://localhost:8333/", false, "", "http://localhost:8333"},
		{"no scheme defaults https", "s3.example.com", false, "", "https://s3.example.com"},
		{"no scheme + DisableSSL", "s3.example.com", true, "", "http://s3.example.com"},
		{"empty endpoint is real AWS", "", false, "", "https://s3.us-east-1.amazonaws.com"},
		{"empty endpoint custom region", "", false, "eu-west-1", "https://s3.eu-west-1.amazonaws.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := newDriver(storage.Config{S3: storage.S3Config{
				Endpoint: c.endpoint, DisableSSL: c.disable, Region: c.region, Bucket: "b",
			}})
			if err != nil {
				t.Fatal(err)
			}
			if d.endpoint != c.want {
				t.Errorf("endpoint = %q, want %q", d.endpoint, c.want)
			}
		})
	}
	if _, err := newDriver(storage.Config{}); err == nil {
		t.Error("empty bucket must fail")
	}
}
