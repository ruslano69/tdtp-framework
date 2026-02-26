package ldap

import (
	"context"
	"fmt"

	goldap "github.com/go-ldap/ldap/v3"
)

type realClient struct {
	conn *goldap.Conn
	cfg  Config
}

// NewRealClient connects to an LDAP/AD server and binds with the service account.
func NewRealClient(cfg Config) (Client, error) {
	conn, err := goldap.DialURL("ldap://" + cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("ldap dial %q: %w", cfg.Addr, err)
	}
	if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ldap bind %q: %w", cfg.BindDN, err)
	}
	return &realClient{conn: conn, cfg: cfg}, nil
}

// IsMember returns true if sAMAccountName=user is directly or transitively a
// member of the given group DN.
func (c *realClient) IsMember(_ context.Context, user, groupDN string) (bool, error) {
	filter := fmt.Sprintf(
		"(&(sAMAccountName=%s)(memberOf:1.2.840.113556.1.4.1941:=%s))",
		goldap.EscapeFilter(user),
		goldap.EscapeFilter(groupDN),
	)
	req := goldap.NewSearchRequest(
		c.cfg.BaseDN,
		goldap.ScopeWholeSubtree,
		goldap.NeverDerefAliases,
		1, 0, false,
		filter,
		[]string{"sAMAccountName"},
		nil,
	)
	result, err := c.conn.Search(req)
	if err != nil {
		return false, fmt.Errorf("ldap search: %w", err)
	}
	return len(result.Entries) > 0, nil
}

func (c *realClient) Close() error {
	c.conn.Close()
	return nil
}
