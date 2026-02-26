// Package ldap provides a thin abstraction over LDAP/AD membership checks.
//
// In dev mode (--dev) use NewMockClient which reads users from a JSON file.
// In production use NewRealClient which binds to a real AD/LDAP server.
// Wrap either with NewCachingClient to cache results in Pipeline Redis.
package ldap

import (
	"context"
	"io"
	"time"
)

// Config holds LDAP connection parameters.
// Used by NewRealClient; MockUsersFile is used by NewMockClient.
type Config struct {
	Addr         string        `yaml:"addr"`          // host:port, e.g. "dc.corp.local:389"
	BindDN       string        `yaml:"bind_dn"`       // e.g. "cn=svc_xzmercury,ou=service,dc=corp,dc=local"
	BindPassword string        `yaml:"bind_password"` // read from env in production
	BaseDN       string        `yaml:"base_dn"`       // e.g. "dc=corp,dc=local"
	CacheTTL     time.Duration `yaml:"cache_ttl"`     // default 120s
	MockUsersFile string       `yaml:"mock_users_file"` // path to JSON file for dev mode
	ACLFile      string        `yaml:"acl_file"`      // path to pipeline-acl.yaml
}

// Client abstracts real and mock LDAP implementations.
type Client interface {
	// IsMember checks whether the given service account belongs to the AD group.
	IsMember(ctx context.Context, user, group string) (bool, error)
	io.Closer
}
