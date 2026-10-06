package main

import (
	"net/url"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/cliconfig"
)

func TestOracleV2RegistrationAndConfig(t *testing.T) {
	if !adapters.IsRegistered("oracle") {
		t.Fatal("Oracle adapter is not registered in v2")
	}
	cfg := cliconfig.CreateSampleConfig("oracle")
	if cfg.Database.Port != 1521 || cfg.Database.Database != "XEPDB1" {
		t.Fatalf("Oracle sample config = %+v", cfg.Database)
	}
	cfg.Database.Password = "secret@with/slash"
	dsn := cfg.Database.BuildDSN()
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "oracle" || u.Host != "localhost:1521" || u.Path != "/XEPDB1" {
		t.Fatalf("Oracle DSN = %q, %v", dsn, err)
	}
	password, _ := u.User.Password()
	if password != cfg.Database.Password {
		t.Fatalf("Oracle password was not escaped correctly: %q", dsn)
	}
}
