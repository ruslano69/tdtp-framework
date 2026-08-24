package processors

import (
	"context"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/mercury"
)

// 32 нулевых байта в base64 — валидный ключ нужной длины; содержимое неважно,
// проверяется путь получения, а не криптостойкость.
const fakeKeyB64 = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

// Биндер, освобождённый от верификации, — как DevClient.
type exemptBinder struct{}

func (exemptBinder) BindKey(_ context.Context, _, _ string) (*mercury.KeyBinding, error) {
	return &mercury.KeyBinding{
		KeyB64: fakeKeyB64,
		HMAC:   "placeholder-not-a-signature",
	}, nil
}
func (exemptBinder) HMACVerificationExempt() bool { return true }

// Обычный биндер, не объявляющий освобождения.
type plainBinder struct{}

func (plainBinder) BindKey(_ context.Context, _, _ string) (*mercury.KeyBinding, error) {
	return &mercury.KeyBinding{
		KeyB64: fakeKeyB64,
		HMAC:   "placeholder-not-a-signature",
	}, nil
}

// Освобождённый биндер работает с ЛЮБЫМ секретом, включая настоящий и пустой.
//
// Ради этого всё и делалось: раньше обход включался строкой "dev-mode" в
// MERCURY_SERVER_SECRET, и на боевом сервере — где секрет выставлен всегда —
// --enc-dev тихо отказывал, деградируя в error-пакет. То есть не работал ровно
// в той аварии, для которой предназначен.
func TestBindAndDecodeKey_ExemptBinderIgnoresTheSecret(t *testing.T) {
	for _, secret := range []string{"dev-mode", "настоящий-боевой-секрет", ""} {
		f := NewFileEncryptor(exemptBinder{}, secret, "UUID-1", "p")
		key, code, err := f.bindAndDecodeKey(context.Background())
		if err != nil {
			t.Errorf("secret=%q: %v (code=%s)", secret, err, code)
			continue
		}
		if len(key) != 32 {
			t.Errorf("secret=%q: ключ %d байт, ожидалось 32", secret, len(key))
		}
	}
}

// А необъявленный биндер проверяется как раньше: умолчание — «проверять».
// Реализации, ничего не знающие об освобождении, обязаны попадать в безопасную
// сторону, а не в удобную.
func TestBindAndDecodeKey_PlainBinderStillVerified(t *testing.T) {
	f := NewFileEncryptor(plainBinder{}, "настоящий-боевой-секрет", "UUID-1", "p")
	_, code, err := f.bindAndDecodeKey(context.Background())
	if err == nil {
		t.Fatal("подставной HMAC принят при настоящем секрете")
	}
	if code != mercury.ErrCodeHMACVerificationFailed {
		t.Errorf("код %q, ожидался %q", code, mercury.ErrCodeHMACVerificationFailed)
	}
}

// Пустой секрет у необъявленного биндера остаётся ошибкой конфигурации.
func TestBindAndDecodeKey_PlainBinderRefusesEmptySecret(t *testing.T) {
	f := NewFileEncryptor(plainBinder{}, "", "UUID-1", "p")
	if _, _, err := f.bindAndDecodeKey(context.Background()); err == nil {
		t.Fatal("пустой секрет принят — молчаливый bypass в проде")
	}
}

// Сентинел "dev-mode" продолжает работать: на нём стоят тестовые серверы и он
// описан как явный опт-аут.
func TestBindAndDecodeKey_DevModeSentinelStillWorks(t *testing.T) {
	f := NewFileEncryptor(plainBinder{}, "dev-mode", "UUID-1", "p")
	if _, _, err := f.bindAndDecodeKey(context.Background()); err != nil {
		t.Fatalf("сентинел перестал работать: %v", err)
	}
}
