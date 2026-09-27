//go:build !nos3

package main

import (
	// Register S3 storage driver so s3:// URIs work (mirrors cmd/tdtpcli).
	_ "github.com/ruslano69/tdtp-framework/pkg/storage/s3"
)
