package s3

import (
	"context"
	"net/http"

	"github.com/ruslano69/tdtp-framework/pkg/storage"
)

// ProbeBucket asks whether the bucket exists and can be reached, with a HEAD
// request. Unlike EnsureBucket it never creates anything: a probe that created
// buckets would change the storage it was meant to look at.
func ProbeBucket(ctx context.Context, cfg storage.Config) error {
	d, err := newDriver(cfg)
	if err != nil {
		return err
	}
	resp, err := d.do(ctx, http.MethodHead, canonicalURI(d.bucket, ""), "", nil, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}
