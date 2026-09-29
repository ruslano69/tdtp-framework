package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// encryptedBlobHeader — валидная шапка зашифрованного блоба
// ([2B ver][1B algo][16B uuid][12B nonce]), достаточная для IsEncryptedBlob.
func encryptedBlobHeader() []byte {
	blob := make([]byte, 2+1+16+12+16)
	blob[0], blob[1], blob[2] = 0x01, 0x00, 0x01
	for i := 3; i < 3+16; i++ {
		blob[i] = byte(i)
	}
	return blob
}

// --to-tdtp не расшифровывает: .enc должен отвергаться по имени, ДО чтения
// ключа xZMercury.
func TestToTDTPRejectsEncFile(t *testing.T) {
	err := rejectEncryptedFile("payroll.tdtp.enc", []byte("<?xml version=\"1.0\"?><DataPacket/>"))
	if err == nil {
		t.Fatal("expected .tdtp.enc to be rejected")
	}
	if !strings.Contains(err.Error(), "does not decrypt") {
		t.Errorf("error should say the command does not decrypt, got: %v", err)
	}
}

// Переименование .enc → .tdtp.xml не должно быть обходом: детект по содержимому.
func TestToTDTPRejectsRenamedEncBlob(t *testing.T) {
	err := rejectEncryptedFile("looks-like-plaintext.tdtp.xml", encryptedBlobHeader())
	if err == nil {
		t.Fatal("expected an encrypted blob to be rejected regardless of its file name")
	}
}

func TestToTDTPAcceptsPlainFile(t *testing.T) {
	if err := rejectEncryptedFile("plain.tdtp.xml", []byte(`<?xml version="1.0"?><DataPacket/>`)); err != nil {
		t.Errorf("plain file must pass: %v", err)
	}
}

// v1.5 держит шифротекст внутри разбираемого пакета — ловится уже после Parse.
func TestToTDTPRejectsEncryptedSections(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		data   string
		want   string
	}{
		{"data only", "", "aes-256-gcm", "Data is encrypted"},
		{"schema only", "aes-256-gcm", "", "Schema is encrypted"},
		{"both", "aes-256-gcm", "aes-256-gcm", "Schema and Data are encrypted"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkt := &packet.DataPacket{}
			pkt.Schema.Encryption = tc.schema
			pkt.Data.Encryption = tc.data

			err := rejectEncryptedSections(pkt)
			if err == nil {
				t.Fatalf("expected rejection for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error should mention %q, got: %v", tc.want, err)
			}
		})
	}
}

func TestToTDTPAcceptsPlainSections(t *testing.T) {
	if err := rejectEncryptedSections(&packet.DataPacket{}); err != nil {
		t.Errorf("unencrypted packet must pass: %v", err)
	}
}

// Сквозная проверка: команда обязана отказать и НЕ тронуть входной файл —
// именно перезапись .enc открытым текстом была исходной проблемой.
func TestConvertTDTPToTDTPLeavesEncFileIntact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.tdtp.enc")
	original := encryptedBlobHeader()
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	err := ConvertTDTPToTDTP(context.Background(), ToTDTPOptions{InputFile: path})
	if err == nil {
		t.Fatal("expected --to-tdtp to refuse an encrypted input")
	}

	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("input file must still exist: %v", readErr)
	}
	if string(after) != string(original) {
		t.Error("input file was modified despite the refusal")
	}
}
