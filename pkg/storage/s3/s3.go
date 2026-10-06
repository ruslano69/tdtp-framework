//go:build !nos3

// Package s3 provides S3-compatible object storage for the TDTP framework.
//
// The client is implemented on net/http alone (SigV4 in sign.go): Put, Get,
// Head, ListV2, Delete and CreateBucket. No AWS SDK — the exchange here is
// small packets ("скачать/записать"), the full SDK only added ~4.5 MB of
// credential-chain and multipart code this driver never uses.
package s3

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/storage"
)

func init() {
	storage.Register("s3", NewDriver)
}

// Error is an S3 request failure: HTTP status plus the <Code>/<Message>
// from the XML error body when the server sent one.
type Error struct {
	Op         string // e.g. "Put key", "Get key"
	StatusCode int
	Code       string
	Message    string
}

func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("s3: %s: %s (status %d, code %s)", e.Op, e.Message, e.StatusCode, e.Code)
	}
	return fmt.Sprintf("s3: %s: status %d", e.Op, e.StatusCode)
}

// Driver implements storage.ObjectStorage over an S3-compatible API.
type Driver struct {
	client   *http.Client
	endpoint string // scheme://host, no trailing slash
	host     string // endpoint host as sent on the wire (port included)
	region   string
	bucket   string
	access   string
	secret   string
}

// newDriver builds the driver without any network I/O.
func newDriver(cfg storage.Config) (*Driver, error) {
	if cfg.S3.Bucket == "" {
		return nil, fmt.Errorf("s3: bucket must not be empty")
	}
	region := cfg.S3.Region
	if region == "" {
		region = "us-east-1"
	}
	endpoint := strings.TrimRight(cfg.S3.Endpoint, "/")
	if endpoint == "" {
		endpoint = "https://s3." + region + ".amazonaws.com"
	} else if !strings.Contains(endpoint, "://") {
		scheme := "https"
		if cfg.S3.DisableSSL {
			scheme = "http"
		}
		endpoint = scheme + "://" + endpoint
	}
	host := endpoint[strings.Index(endpoint, "://")+3:]
	if i := strings.Index(host, "/"); i >= 0 {
		host = host[:i]
	}
	return &Driver{
		client:   &http.Client{},
		endpoint: endpoint,
		host:     host,
		region:   region,
		bucket:   cfg.S3.Bucket,
		access:   cfg.S3.AccessKey,
		secret:   cfg.S3.SecretKey,
	}, nil
}

// NewDriver creates an S3 driver from the given Config.
func NewDriver(cfg storage.Config) (storage.ObjectStorage, error) {
	return newDriver(cfg)
}

// EnsureBucket creates bucket when missing; an already-owned bucket is OK.
// Used by test setup and ops tooling — not part of ObjectStorage.
func EnsureBucket(ctx context.Context, cfg storage.Config) error {
	d, err := newDriver(cfg)
	if err != nil {
		return err
	}
	return d.ensureBucket(ctx)
}

// do sends one signed request. uri/query are the canonical forms; body may
// be nil. On 2xx the caller owns resp.Body, otherwise do consumes it and
// returns *Error.
func (d *Driver) do(ctx context.Context, method, uri, query string, body []byte, headers map[string]string) (*http.Response, error) {
	url := d.endpoint + uri
	if query != "" {
		url += "?" + query
	}
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("s3: %s %s: %w", method, uri, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	payloadHash := sha256Hex(body)
	signHTTPRequest(req, uri, query, d.host, payloadHash, d.access, d.secret, d.region, time.Now())

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3: %s %s: %w", method, uri, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	s3err := readErrorResponse(resp)
	s3err.Op = method + " " + uri
	return nil, s3err
}

type xmlErrorBody struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}

// readErrorResponse drains resp.Body (always closed) and parses the S3 XML
// error; falls back to the HTTP status text.
func readErrorResponse(resp *http.Response) *Error {
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
	s3err := &Error{StatusCode: resp.StatusCode}
	var body xmlErrorBody
	if xml.Unmarshal(raw, &body) == nil && body.Code != "" {
		s3err.Code = body.Code
		s3err.Message = body.Message
	} else {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		s3err.Message = msg
		// S3 HeadObject errors carry no XML body — only the status.
		if resp.StatusCode == http.StatusNotFound {
			s3err.Code = "NotFound"
		}
	}
	return s3err
}

// Put streams reader to S3 key, attaching meta as x-amz-meta-tdtp-* headers.
// Small packets (≤ a few MB) go as a single PutObject — no multipart, no
// temp files.
func (d *Driver) Put(ctx context.Context, key string, reader io.Reader, meta map[string]string) error {
	body, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("s3: Put %s: %w", key, err)
	}
	headers := make(map[string]string, len(meta))
	for k, v := range meta {
		headers["X-Amz-Meta-Tdtp-"+k] = v
	}
	resp, err := d.do(ctx, http.MethodPut, canonicalURI(d.bucket, key), "", body, headers)
	if err != nil {
		if s3err, ok := err.(*Error); ok {
			s3err.Op = "Put " + key
			return s3err
		}
		return fmt.Errorf("s3: Put %s: %w", key, err)
	}
	_ = resp.Body.Close()
	return nil
}

// Get returns a ReadCloser for the object at key. Caller must close it.
func (d *Driver) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	resp, err := d.do(ctx, http.MethodGet, canonicalURI(d.bucket, key), "", nil, nil)
	if err != nil {
		if s3err, ok := err.(*Error); ok {
			s3err.Op = "Get " + key
			return nil, s3err
		}
		return nil, fmt.Errorf("s3: Get %s: %w", key, err)
	}
	return resp.Body, nil
}

// Stat returns metadata for the object at key.
func (d *Driver) Stat(ctx context.Context, key string) (*storage.ObjectInfo, error) {
	resp, err := d.do(ctx, http.MethodHead, canonicalURI(d.bucket, key), "", nil, nil)
	if err != nil {
		if s3err, ok := err.(*Error); ok {
			s3err.Op = "Stat " + key
			return nil, s3err
		}
		return nil, fmt.Errorf("s3: Stat %s: %w", key, err)
	}
	defer func() { _ = resp.Body.Close() }()

	info := &storage.ObjectInfo{Key: key, Metadata: map[string]string{}}
	if n := resp.Header.Get("Content-Length"); n != "" {
		if size, err := strconv.ParseInt(n, 10, 64); err == nil {
			info.Size = size
		}
	}
	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		if t, err := time.Parse(http.TimeFormat, lm); err == nil {
			info.ModTime = t
		}
	}
	for k, vv := range resp.Header {
		if strings.HasPrefix(k, "X-Amz-Meta-") && len(vv) > 0 {
			info.Metadata[strings.ToLower(strings.TrimPrefix(k, "X-Amz-Meta-"))] = vv[0]
		}
	}
	return info, nil
}

type listedObject struct {
	Key          string `xml:"Key"`
	Size         int64  `xml:"Size"`
	LastModified string `xml:"LastModified"`
}

type listBucketResult struct {
	XMLName               xml.Name       `xml:"ListBucketResult"`
	IsTruncated           bool           `xml:"IsTruncated"`
	NextContinuationToken string         `xml:"NextContinuationToken"`
	Contents              []listedObject `xml:"Contents"`
}

// List returns all objects with the given prefix.
func (d *Driver) List(ctx context.Context, prefix string) ([]storage.ObjectInfo, error) {
	var result []storage.ObjectInfo
	var token string
	for {
		params := [][2]string{
			{"list-type", "2"},
			{"max-keys", "1000"},
			{"prefix", prefix},
		}
		if token != "" {
			params = append(params, [2]string{"continuation-token", token})
		}
		query := canonicalQuery(params)
		resp, err := d.do(ctx, http.MethodGet, canonicalURI(d.bucket, ""), query, nil, nil)
		if err != nil {
			if s3err, ok := err.(*Error); ok {
				s3err.Op = "List prefix=" + prefix
				return nil, s3err
			}
			return nil, fmt.Errorf("s3: List prefix=%s: %w", prefix, err)
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("s3: List prefix=%s: %w", prefix, err)
		}
		var page listBucketResult
		if err := xml.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("s3: List prefix=%s: bad XML: %w", prefix, err)
		}
		for _, obj := range page.Contents {
			info := storage.ObjectInfo{Key: obj.Key, Size: obj.Size}
			if obj.LastModified != "" {
				if t, err := time.Parse(time.RFC3339, obj.LastModified); err == nil {
					info.ModTime = t
				}
			}
			result = append(result, info)
		}
		if !page.IsTruncated {
			return result, nil
		}
		token = page.NextContinuationToken
		if token == "" {
			return result, nil
		}
	}
}

// Delete removes the object at key.
func (d *Driver) Delete(ctx context.Context, key string) error {
	resp, err := d.do(ctx, http.MethodDelete, canonicalURI(d.bucket, key), "", nil, nil)
	if err != nil {
		if s3err, ok := err.(*Error); ok {
			s3err.Op = "Delete " + key
			return s3err
		}
		return fmt.Errorf("s3: Delete %s: %w", key, err)
	}
	_ = resp.Body.Close()
	return nil
}

// ensureBucket creates the bucket; already-owned is success.
func (d *Driver) ensureBucket(ctx context.Context) error {
	resp, err := d.do(ctx, http.MethodPut, canonicalURI(d.bucket, ""), "", nil, nil)
	if err != nil {
		if s3err, ok := err.(*Error); ok {
			if s3err.Code == "BucketAlreadyOwnedByYou" || s3err.Code == "BucketAlreadyExists" {
				return nil
			}
			s3err.Op = "CreateBucket " + d.bucket
		}
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// Close is a no-op; the client is stateless.
func (d *Driver) Close() error { return nil }
