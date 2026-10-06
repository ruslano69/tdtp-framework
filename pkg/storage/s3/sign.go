// sign.go — minimal AWS Signature Version 4 for S3, stdlib only.
//
// Signs host, x-amz-content-sha256, x-amz-date plus any other x-amz-*
// headers the request carries (e.g. x-amz-meta-*): S3 implementations
// recompute over every x-amz-* header received, so anything sent is signed.
//
// Golden vectors live in s3_test.go; they were produced by an independent
// Python implementation written straight from the AWS spec text.

package s3

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	sigAlgorithm  = "AWS4-HMAC-SHA256"
	sigService    = "s3"
	sigTerminator = "aws4_request"
)

// isUnreserved reports whether c may travel unescaped in a SigV4
// canonical URI or query string: A-Za-z0-9 plus - _ . ~
func isUnreserved(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	}
	return c == '-' || c == '_' || c == '.' || c == '~'
}

// escapeSigV4 percent-encodes s per SigV4 rules (uppercase hex, UTF-8
// bytes encoded individually). If keepSlash is true, '/' passes through —
// used for canonical URIs, never for query strings.
func escapeSigV4(s string, keepSlash bool) string {
	var b strings.Builder
	b.Grow(len(s) + len(s)/8)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUnreserved(c) || (keepSlash && c == '/') {
			b.WriteByte(c)
			continue
		}
		const digits = "0123456789ABCDEF"
		b.WriteByte('%')
		b.WriteByte(digits[c>>4])
		b.WriteByte(digits[c&0x0f])
	}
	return b.String()
}

// canonicalURI builds the path-style canonical URI: /<bucket>/<key>,
// segments encoded, '/' separators preserved.
func canonicalURI(bucket, key string) string {
	if key == "" {
		return "/" + escapeSigV4(bucket, false) + "/"
	}
	segs := strings.Split(key, "/")
	for i := range segs {
		segs[i] = escapeSigV4(segs[i], false)
	}
	return "/" + escapeSigV4(bucket, false) + "/" + strings.Join(segs, "/")
}

// canonicalQuery encodes, sorts and joins query parameters.
func canonicalQuery(params [][2]string) string {
	type kv struct{ k, v string }
	enc := make([]kv, 0, len(params))
	for _, p := range params {
		enc = append(enc, kv{escapeSigV4(p[0], false), escapeSigV4(p[1], false)})
	}
	sort.Slice(enc, func(i, j int) bool {
		if enc[i].k != enc[j].k {
			return enc[i].k < enc[j].k
		}
		return enc[i].v < enc[j].v
	})
	parts := make([]string, 0, len(enc))
	for _, e := range enc {
		parts = append(parts, e.k+"="+e.v)
	}
	return strings.Join(parts, "&")
}

func hmacSHA256(key []byte, msg string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(msg))
	return h.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// signV4 computes the Authorization header value for an S3 request.
// uri and query must already be canonical (see canonicalURI/canonicalQuery);
// host is the endpoint host exactly as sent on the wire (port included).
// extra carries additional x-amz-* headers to sign (e.g. x-amz-meta-*);
// S3 implementations recompute over every x-amz-* header they receive,
// so anything we send must be signed. Names are lowercased, values trimmed.
func signV4(method, uri, query, host, payloadHash string, extra map[string]string, accessKey, secretKey, region string, t time.Time) string {
	t = t.UTC()
	amzDate := t.Format("20060102T150405Z")
	dateStamp := t.Format("20060102")

	names := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	values := map[string]string{
		"host":                 host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           amzDate,
	}
	for k, v := range extra {
		lk := strings.ToLower(k)
		if _, dup := values[lk]; dup {
			continue
		}
		names = append(names, lk)
		values[lk] = strings.TrimSpace(v)
	}
	sort.Strings(names)

	var canonHeaders strings.Builder
	for _, n := range names {
		canonHeaders.WriteString(n + ":" + values[n] + "\n")
	}
	signedHeaders := strings.Join(names, ";")

	canonReq := method + "\n" + uri + "\n" + query + "\n" +
		canonHeaders.String() + "\n" + signedHeaders + "\n" + payloadHash

	scope := dateStamp + "/" + region + "/" + sigService + "/" + sigTerminator
	stringToSign := sigAlgorithm + "\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonReq))

	k := hmacSHA256([]byte("AWS4"+secretKey), dateStamp)
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, sigService)
	k = hmacSHA256(k, sigTerminator)
	signature := hex.EncodeToString(hmacSHA256(k, stringToSign))

	return sigAlgorithm + " Credential=" + accessKey + "/" + scope +
		", SignedHeaders=" + signedHeaders + ", Signature=" + signature
}

// signHTTPRequest stamps SigV4 headers onto req. uri/query/host describe the
// request as it goes on the wire; payloadHash is hex(sha256(body)).
// Any x-amz-* headers already on req (e.g. x-amz-meta-*) join the signature.
func signHTTPRequest(req *http.Request, uri, query, host, payloadHash, accessKey, secretKey, region string, t time.Time) {
	amzDate := t.UTC().Format("20060102T150405Z")
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	extra := map[string]string{}
	for k, vv := range req.Header {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "x-amz-") &&
			lk != "x-amz-date" && lk != "x-amz-content-sha256" &&
			lk != "authorization" && len(vv) > 0 {
			extra[lk] = vv[0]
		}
	}
	req.Header.Set("Authorization", signV4(req.Method, uri, query, host, payloadHash, extra, accessKey, secretKey, region, t))
}
