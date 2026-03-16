package ldap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
)

// mockUser is one entry in the JSON users file.
type mockUser struct {
	Username string   `json:"username"`
	Groups   []string `json:"groups"`
}

type mockClient struct {
	users map[string]mockUser
}

// NewMockClient creates a Client backed by a JSON file.
// If usersFile is empty, built-in dev defaults are used:
//
//	svc_tdtp  → [tdtp-pipeline-users, tdtp-admins]
//	analyst1  → [tdtp-pipeline-users]
func NewMockClient(usersFile string) (Client, error) {
	mc := &mockClient{users: make(map[string]mockUser)}

	if usersFile == "" {
		for _, u := range []mockUser{
			{Username: "svc_tdtp", Groups: []string{"tdtp-pipeline-users", "tdtp-admins"}},
			{Username: "analyst1", Groups: []string{"tdtp-pipeline-users"}},
			{Username: "readonly", Groups: []string{"tdtp-readonly"}},
		} {
			mc.users[u.Username] = u
		}
		return mc, nil
	}

	data, err := os.ReadFile(usersFile)
	if err != nil {
		return nil, fmt.Errorf("mock ldap: read users file %q: %w", usersFile, err)
	}
	var users []mockUser
	if err := json.Unmarshal(data, &users); err != nil {
		return nil, fmt.Errorf("mock ldap: parse users file: %w", err)
	}
	for _, u := range users {
		mc.users[u.Username] = u
	}
	return mc, nil
}

func (mc *mockClient) IsMember(_ context.Context, user, group string) (bool, error) {
	u, ok := mc.users[user]
	if !ok {
		return false, nil
	}
	return slices.Contains(u.Groups, group), nil
}

func (mc *mockClient) Close() error { return nil }
