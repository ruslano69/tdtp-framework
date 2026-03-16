//go:build !production

package mercury

import (
	"context"
	"testing"
)

func TestDevClient_BindKey_ReturnsValidKey(t *testing.T) {
	client := NewDevClient()
	binding, err := client.BindKey(context.Background(), "test-uuid", "test-pipeline")
	if err != nil {
		t.Fatalf("DevClient.BindKey() error = %v", err)
	}
	if binding == nil {
		t.Fatal("DevClient.BindKey() returned nil binding")
	}

	// Ключ должен быть валидным AES-256 (32 байта)
	key, err := DecodeKey(binding.KeyB64)
	if err != nil {
		t.Fatalf("DevClient.BindKey() returned invalid key_b64: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("DevClient.BindKey() key length = %d, want 32", len(key))
	}
}

func TestDevClient_BindKey_UniqueKeysPerCall(t *testing.T) {
	// Каждый вызов должен генерировать уникальный ключ
	client := NewDevClient()
	b1, err := client.BindKey(context.Background(), "uuid-1", "pipeline")
	if err != nil {
		t.Fatalf("first BindKey() error = %v", err)
	}
	b2, err := client.BindKey(context.Background(), "uuid-2", "pipeline")
	if err != nil {
		t.Fatalf("second BindKey() error = %v", err)
	}
	if b1.KeyB64 == b2.KeyB64 {
		t.Error("DevClient.BindKey() returned same key twice — keys must be random")
	}
}

func TestVerifyHMACDev_AlwaysTrue(t *testing.T) {
	// В dev-режиме HMAC-верификация всегда проходит (ключ не покидает процесс)
	tests := []struct {
		name           string
		uuid, hmac, secret string
	}{
		{"valid looking", "uuid-1", "deadbeef", "secret"},
		{"empty all", "", "", ""},
		{"wrong hmac", "uuid-2", "totally-wrong", "secret"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !VerifyHMACDev(tt.uuid, tt.hmac, tt.secret) {
				t.Error("VerifyHMACDev() returned false — must always return true in dev mode")
			}
		})
	}
}
