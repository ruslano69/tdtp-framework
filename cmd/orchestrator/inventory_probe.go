package main

// inventory_probe.go — the probes themselves, and the classification of what
// they return.
//
// A probe answers one question about one endpoint and changes nothing there:
// a database is connected to and closed, a bucket is asked about with HEAD, a
// queue is checked with a passive declare, a file is stat-ed. Nothing is
// created, written or consumed.

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/brokers"
	"github.com/ruslano69/tdtp-framework/pkg/storage"
	s3store "github.com/ruslano69/tdtp-framework/pkg/storage/s3"
)

// probeTimeout bounds one probe. A probe that cannot honour its context (an
// amqp dial, for one) is abandoned at this point; it finishes on its own and
// releases what it holds.
const probeTimeout = 5 * time.Second

var (
	errProbeTimeout = errors.New("probe timed out")
	errNotDir       = errors.New("not a directory")
	errNoQueue      = errors.New("no queue configured for the broker")
)

// runProbe runs one probe under the deadline.
func runProbe(ctx context.Context, probe func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	done := make(chan error, 1) // buffered: an abandoned probe must not block forever
	go func() { done <- probe(ctx) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return errProbeTimeout
	}
}

func adaptersNew(ctx context.Context, typ, dsn string) (adapters.Adapter, error) {
	return adapters.New(ctx, adapters.Config{Type: typ, DSN: dsn})
}

func statLocal(path string) error {
	_, err := os.Stat(path)
	return err
}

func statLocalDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errNotDir
	}
	return nil
}

// s3Settings builds the storage config from a pipeline's s3 block. Without a
// block the driver's defaults apply, which is the AWS endpoint.
func s3Settings(bucket string, cfg *invS3) storage.Config {
	sc := storage.S3Config{Bucket: bucket}
	if cfg != nil {
		sc.Endpoint = cfg.Endpoint
		sc.Region = cfg.Region
		sc.AccessKey = cfg.AccessKey
		sc.SecretKey = cfg.SecretKey
		sc.DisableSSL = cfg.DisableSSL
	}
	return storage.Config{Type: "s3", S3: sc}
}

func statS3Object(ctx context.Context, cfg storage.Config, key string) error {
	d, err := s3store.NewDriver(cfg)
	if err != nil {
		return err
	}
	_, err = d.Stat(ctx, key)
	return err
}

func probeS3Bucket(ctx context.Context, cfg storage.Config) error {
	return s3store.ProbeBucket(ctx, cfg)
}

func probeRabbitMQ(r invRabbitMQ, port int, vhost string) error {
	if r.Queue == "" {
		return errNoQueue
	}
	b, err := brokers.NewRabbitMQ(brokers.Config{
		Type:           "rabbitmq",
		Host:           r.Host,
		Port:           port,
		User:           r.User,
		Password:       r.Password,
		VHost:          vhost,
		Queue:          r.Queue,
		PassiveDeclare: true, // the queue must exist; the probe never creates it
	})
	if err != nil {
		return err
	}
	if err := b.Connect(context.Background()); err != nil {
		return err
	}
	return b.Close()
}

func probeKafka(ctx context.Context, k invKafka) error {
	b, err := brokers.New(brokers.Config{Type: "kafka", Brokers: k.Brokers, Topic: k.Topic})
	if err != nil {
		return err
	}
	defer func() { _ = b.Close() }()
	return b.Ping(ctx)
}

func probeRedis(ctx context.Context, r invResultLog) error {
	c := redis.NewClient(&redis.Options{
		Addr:        r.Address,
		Password:    r.Password,
		DB:          r.DB,
		DialTimeout: probeTimeout,
	})
	defer func() { _ = c.Close() }()
	return c.Ping(ctx).Err()
}

// ─── classification ──────────────────────────────────────────────────────────

// Driver error texts, lower-cased. They are matched, never shown: the class is
// what leaves the probe. Order matters: an authentication failure can also say
// "not found" in some drivers, so auth is checked first.
var (
	authMarkers = []string{
		"password authentication failed", "access denied", "login failed",
		"ora-01017", "invalid username/password", "authentication failed",
		"invalid credentials", "access_refused", "nosuchkey", "accessdenied",
	}
	notFoundMarkers = []string{
		"does not exist", "unknown database", "no such file", "ora-12514",
		"cannot open database", "database not found", "nosuchbucket",
		"not found", "404",
	}
	networkMarkers = []string{
		"connection refused", "no such host", "i/o timeout", "connectex",
		"dial tcp", "network is unreachable", "connection reset", "server closed",
		"broken pipe", "no connection could be made", "eof",
	}
)

// classifyProbeErr returns the failure class of a probe error. Only the class
// is stored or shown: an error text can name a host, a user or a path.
func classifyProbeErr(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, errProbeTimeout) || errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, errNoQueue) || errors.Is(err, errNotDir) {
		return "config"
	}

	var se *s3store.Error
	if errors.As(err, &se) {
		switch se.StatusCode {
		case 404:
			return "not_found"
		case 401, 403:
			return "auth"
		}
		return "other"
	}
	if errors.Is(err, fs.ErrNotExist) {
		return "not_found"
	}
	if errors.Is(err, fs.ErrPermission) {
		return "permission"
	}
	if class, _ := brokers.ErrorClass(err); class != "network" {
		return class // an amqp code the broker layer already knows
	}

	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	text := strings.ToLower(err.Error())
	if containsAny(text, authMarkers) {
		return "auth"
	}
	if containsAny(text, notFoundMarkers) {
		return "not_found"
	}
	if containsAny(text, networkMarkers) {
		return "network"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return "network"
	}
	return "other"
}

func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
