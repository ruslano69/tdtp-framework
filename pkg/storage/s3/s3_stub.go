//go:build nos3

package s3

import (
	"errors"

	"github.com/ruslano69/tdtp-framework/pkg/storage"
)

func init() {
	storage.Register("s3", func(_ storage.Config) (storage.ObjectStorage, error) {
		return nil, errors.New("S3 support is disabled in this build (-tags nos3)")
	})
}
