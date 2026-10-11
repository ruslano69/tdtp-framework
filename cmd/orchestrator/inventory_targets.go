package main

// inventory_targets.go — which endpoints a scenario depends on.
//
// The scenario's own YAML is the source of truth. It is rendered with its
// parameters (defaults applied, magic values such as {{SYNC_TS}} filled in) and
// read for its sources, its output and its result log. The inventory never runs
// a scenario and never reads data; it only asks whether each endpoint answers.
//
// A target has a key and a probe. The key is what is stored and shown: it names
// the kind of endpoint and where it is, and carries no credentials. The probe is
// a closure that holds the credentials it needs, and is never stored or logged.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// invTarget is one endpoint a scenario uses.
type invTarget struct {
	Key   string // shown and stored: no userinfo, no password, no token
	Kind  string // db | file | s3 | broker | redis | dir
	ident string // fingerprint of the probe's full identity, secrets included; in memory only
	probe func(ctx context.Context) error
}

// hashIdent fingerprints what identifies one probe. Two scenarios that reach the
// same server with different credentials get different identities, so they are
// probed separately even though they share a key. The hash never leaves the
// process.
func hashIdent(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// unresolvedError says a scenario's targets cannot be read yet: a parameter has
// no value, or the pipeline does not render. That is not a failure of an
// endpoint; the scenario may run later with the right values. class is what the
// snapshot stores; detail is for the orchestrator's log only.
type unresolvedError struct {
	class  string // param | template | parse
	detail string
}

func (e *unresolvedError) Error() string { return "unresolved (" + e.class + ")" }

// invPipeline is the part of a pipeline YAML the inventory reads. It is kept
// separate from pkg/etl on purpose: that package links every connector, and the
// orchestrator avoids them (see pubsub.go).
type invPipeline struct {
	Sources   []invSource  `yaml:"sources"`
	Output    invOutput    `yaml:"output"`
	ResultLog invResultLog `yaml:"result_log"`
}

type invSource struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
	DSN  string `yaml:"dsn"`
	S3   *invS3 `yaml:"s3"`
}

type invS3 struct {
	Endpoint   string `yaml:"endpoint"`
	Region     string `yaml:"region"`
	Bucket     string `yaml:"bucket"`
	AccessKey  string `yaml:"access_key"`
	SecretKey  string `yaml:"secret_key"`
	DisableSSL bool   `yaml:"disable_ssl"`
}

type invOutput struct {
	Type     string       `yaml:"type"`
	TDTP     *invDest     `yaml:"tdtp"`
	XLSX     *invDest     `yaml:"xlsx"`
	RabbitMQ *invRabbitMQ `yaml:"rabbitmq"`
	Kafka    *invKafka    `yaml:"kafka"`
}

type invDest struct {
	Destination string `yaml:"destination"`
	S3          *invS3 `yaml:"s3"`
}

type invRabbitMQ struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Queue    string `yaml:"queue"`
	VHost    string `yaml:"vhost"`
}

type invKafka struct {
	Brokers []string `yaml:"brokers"`
	Topic   string   `yaml:"topic"`
}

type invResultLog struct {
	Type     string `yaml:"type"`
	Address  string `yaml:"address"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// deriveTargets lists the endpoints of one scenario. params are the caller's
// values; defaults are applied by ValidateParams. A failure is an
// *unresolvedError.
func deriveTargets(s *Scenario, params map[string]string) ([]invTarget, error) {
	resolved, err := s.ValidateParams(params)
	if err != nil {
		return nil, &unresolvedError{class: "param", detail: err.Error()}
	}
	data := resolveMagicParams(resolved, "")
	rendered, err := renderTemplate(s.RawYAML, data)
	if err != nil {
		return nil, &unresolvedError{class: "template", detail: "template does not render"}
	}
	var p invPipeline
	if err := yaml.Unmarshal(rendered, &p); err != nil {
		// The yaml error text can quote a value, so only the fact is kept.
		return nil, &unresolvedError{class: "parse", detail: "pipeline does not parse"}
	}

	var out []invTarget
	for _, src := range p.Sources {
		if t, ok := sourceTarget(src); ok {
			out = append(out, t)
		}
	}
	out = append(out, outputTargets(p.Output)...)
	if t, ok := resultLogTarget(p.ResultLog); ok {
		out = append(out, t)
	}
	return out, nil
}

func sourceTarget(src invSource) (invTarget, bool) {
	typ := strings.ToLower(src.Type)
	switch typ {
	case "postgres", "mysql", "mssql", "oracle":
		return dbTarget(typ, src.DSN), true
	case "sqlite":
		return fileTarget(stripSQLiteParams(src.DSN)), true
	case "csv", "tdtp", "tdtp-enc", "tdtp-s3":
		if strings.HasPrefix(src.DSN, "s3://") {
			return s3ObjectTarget(src.DSN, src.S3)
		}
		if src.DSN == "" {
			return invTarget{}, false
		}
		return fileTarget(src.DSN), true
	}
	return invTarget{}, false
}

func outputTargets(o invOutput) []invTarget {
	var out []invTarget
	switch strings.ToLower(o.Type) {
	case "rabbitmq":
		if o.RabbitMQ != nil {
			out = append(out, rabbitMQTarget(*o.RabbitMQ))
		}
	case "kafka":
		if o.Kafka != nil {
			out = append(out, kafkaTarget(*o.Kafka))
		}
	case "tdtp":
		if o.TDTP != nil {
			out = append(out, destinationTarget(o.TDTP.Destination, o.TDTP.S3))
		}
	case "xlsx":
		if o.XLSX != nil {
			out = append(out, destinationTarget(o.XLSX.Destination, nil))
		}
	}
	return out
}

func resultLogTarget(r invResultLog) (invTarget, bool) {
	if strings.ToLower(r.Type) != "redis" || r.Address == "" {
		return invTarget{}, false
	}
	return redisTarget(r), true
}

// ─── keys and probes ─────────────────────────────────────────────────────────

var (
	mysqlTCP  = regexp.MustCompile(`tcp\(([^)]*)\)/([^?]*)`)
	mssqlSrv  = regexp.MustCompile(`(?i)server=([^;]+)`)
	mssqlDB   = regexp.MustCompile(`(?i)database=([^;]+)`)
	sqlParams = regexp.MustCompile(`\?.*$`)
)

// dbTarget builds the key from the endpoint in the DSN and probes by connecting
// through the adapter, which is what a scenario does first.
func dbTarget(typ, dsn string) invTarget {
	key := dbEndpointKey(typ, dsn)
	return invTarget{
		Key:   key,
		Kind:  "db",
		ident: hashIdent("db", typ, dsn),
		probe: func(ctx context.Context) error {
			a, err := adaptersNew(ctx, typ, dsn)
			if err != nil {
				return err
			}
			return a.Close(ctx)
		},
	}
}

// dbEndpointKey returns "type://host:port/database" when the DSN can be read,
// and an opaque key built from a hash of the DSN when it cannot. The hash names
// the DSN without revealing any part of it.
func dbEndpointKey(typ, dsn string) string {
	switch typ {
	case "postgres", "oracle":
		if u, err := url.Parse(dsn); err == nil && u.Host != "" {
			return typ + "://" + u.Host + u.Path
		}
	case "mysql":
		if m := mysqlTCP.FindStringSubmatch(dsn); m != nil {
			return typ + "://" + m[1] + "/" + m[2]
		}
	case "mssql":
		if s := mssqlSrv.FindStringSubmatch(dsn); s != nil {
			db := ""
			if d := mssqlDB.FindStringSubmatch(dsn); d != nil {
				db = d[1]
			}
			return typ + "://" + strings.TrimSpace(s[1]) + "/" + strings.TrimSpace(db)
		}
	}
	sum := sha256.Sum256([]byte(dsn))
	return "db:" + typ + ":" + hex.EncodeToString(sum[:6])
}

// fileTarget probes a local file: it must exist.
func fileTarget(path string) invTarget {
	clean := filepath.Clean(path)
	return invTarget{
		Key:   "file:" + clean,
		Kind:  "file",
		ident: hashIdent("file", clean),
		probe: func(ctx context.Context) error {
			return statLocal(clean)
		},
	}
}

// s3ObjectTarget probes one object. The bucket and key come from the s3:// DSN;
// the connection settings come from the pipeline's s3 block.
func s3ObjectTarget(dsn string, cfg *invS3) (invTarget, bool) {
	bucket, key, ok := splitS3(dsn)
	if !ok {
		return invTarget{}, false
	}
	sc := s3Settings(bucket, cfg)
	return invTarget{
		Key:   "s3://" + bucket + "/" + key,
		Kind:  "s3",
		ident: hashIdent("s3object", bucket, key, sc.S3.Endpoint, sc.S3.AccessKey, sc.S3.SecretKey),
		probe: func(ctx context.Context) error {
			return statS3Object(ctx, sc, key)
		},
	}, true
}

// destinationTarget is an output's destination: an s3:// bucket is probed by
// HEAD on the bucket; a local path by its directory.
func destinationTarget(dest string, cfg *invS3) invTarget {
	if strings.HasPrefix(dest, "s3://") {
		bucket, _, ok := splitS3(dest)
		if ok {
			sc := s3Settings(bucket, cfg)
			return invTarget{
				Key:   "s3://" + bucket,
				Kind:  "s3",
				ident: hashIdent("s3", bucket, sc.S3.Endpoint, sc.S3.AccessKey, sc.S3.SecretKey),
				probe: func(ctx context.Context) error {
					return probeS3Bucket(ctx, sc)
				},
			}
		}
	}
	dir := filepath.Dir(filepath.Clean(dest))
	return invTarget{
		Key:   "dir:" + dir,
		Kind:  "dir",
		ident: hashIdent("dir", dir),
		probe: func(ctx context.Context) error {
			return statLocalDir(dir)
		},
	}
}

func rabbitMQTarget(r invRabbitMQ) invTarget {
	port := r.Port
	if port == 0 {
		port = 5672
	}
	vhost := r.VHost
	if vhost == "" {
		vhost = "/"
	}
	return invTarget{
		Key:   fmt.Sprintf("rabbitmq://%s:%d%s", r.Host, port, vhost),
		Kind:  "broker",
		ident: hashIdent("rabbitmq", r.Host, strconv.Itoa(port), vhost, r.User, r.Password),
		probe: func(context.Context) error {
			return probeRabbitMQ(r, port, vhost)
		},
	}
}

func kafkaTarget(k invKafka) invTarget {
	return invTarget{
		Key:   "kafka://" + strings.Join(k.Brokers, ","),
		Kind:  "broker",
		ident: hashIdent("kafka", strings.Join(k.Brokers, ","), k.Topic),
		probe: func(ctx context.Context) error {
			return probeKafka(ctx, k)
		},
	}
}

func redisTarget(r invResultLog) invTarget {
	return invTarget{
		Key:   fmt.Sprintf("redis://%s/%d", r.Address, r.DB),
		Kind:  "redis",
		ident: hashIdent("redis", r.Address, strconv.Itoa(r.DB), r.Password),
		probe: func(ctx context.Context) error {
			return probeRedis(ctx, r)
		},
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func stripSQLiteParams(dsn string) string {
	return sqlParams.ReplaceAllString(strings.TrimPrefix(dsn, "file:"), "")
}

// splitS3 reads s3://bucket/key into its parts. The key may be empty.
func splitS3(dsn string) (bucket, key string, ok bool) {
	rest := strings.TrimPrefix(dsn, "s3://")
	if rest == dsn || rest == "" {
		return "", "", false
	}
	bucket, key, _ = strings.Cut(rest, "/")
	return bucket, key, bucket != ""
}
