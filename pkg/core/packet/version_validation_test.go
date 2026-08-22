package packet

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"testing"
)

// packetXML собирает минимальный валидный пакет с заданной версией и
// произвольными атрибутами на <Data>.
func packetXML(version, dataAttrs string) []byte {
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="%s"><Header><Type>reference</Type>`+
		`<TableName>t</TableName><MessageID>REF-1</MessageID><PartNumber>1</PartNumber>`+
		`<TotalParts>1</TotalParts><RecordsInPart>1</RecordsInPart>`+
		`<Timestamp>2026-08-22T12:00:00Z</Timestamp></Header>`+
		`<Schema><Field name="id" type="INTEGER"/></Schema>`+
		`<Data%s><R>1</R></Data></DataPacket>`, version, dataAttrs))
}

// Версия обязана быть версией. До этой проверки в поле годилось что угодно
// непустое, и мусор молча доезжал до NeedsRowCountCheck, где неразбираемое
// значение трактуется как новее любого известного, — то есть менял путь
// проверки целостности, ничем себя не обозначив.
func TestValidatePacket_RejectsMalformedVersion(t *testing.T) {
	for _, v := range []string{"abc", "1.x", "1..2", "1.", ".1", "-1.0", "v1.4", "1.4-beta", "1,4"} {
		_, err := NewParser().ParseBytes(packetXML(v, ""))
		if err == nil {
			t.Errorf("version %q: parsed without error, want rejection", v)
			continue
		}
		if !strings.Contains(err.Error(), "invalid version") {
			t.Errorf("version %q: error does not name the problem: %v", v, err)
		}
	}
}

// Незнакомая, но корректная версия принимается — это не послабление, а
// политика совместимости из docs/SPECIFICATION.md → Versioning: читатель
// деградирует на возможностях, а не отказывает по номеру. Там прямо сказано,
// что читатель v1.3.1 читает пакеты v1.4, игнорируя атрибуты xxh3.
func TestValidatePacket_AcceptsUnknownButWellFormedVersion(t *testing.T) {
	for _, v := range []string{"1.6", "1.99", "2.0", "10.0", "3.1.4"} {
		if _, err := NewParser().ParseBytes(packetXML(v, "")); err != nil {
			t.Errorf("version %q: rejected (%v) — an unknown version must still be read", v, err)
		}
	}
}

// captureLog перехватывает вывод log на время вызова fn.
func captureLog(fn func()) string {
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	}()
	fn()
	return buf.String()
}

// Главное требование: сжатый пакет с version="1.0" ЧИТАЕТСЯ. Таких архивов
// накоплено за годы — сжатие появилось в 1.2, но версию пакета никогда не
// поднимало. Предупредить стоит, отвергать нельзя.
func TestValidatePacket_OldCompressedPacketIsReadWithWarning(t *testing.T) {
	var err error
	out := captureLog(func() {
		_, err = NewParser().ParseBytes(packetXML("1.0", ` compression="zstd"`))
	})
	if err != nil {
		t.Fatalf("a compressed 1.0 packet must still be read, got: %v", err)
	}
	if !strings.Contains(out, "WARNING") || !strings.Contains(out, "compression") {
		t.Errorf("expected a warning naming compression, got %q", out)
	}
	if !strings.Contains(out, "1.2") {
		t.Errorf("the warning should say which version introduced the feature, got %q", out)
	}
}

func TestWarnVersionBelowFeatures(t *testing.T) {
	cases := []struct {
		name      string
		version   string
		dataAttrs string
		wantWarn  string // подстрока; пусто — предупреждения быть не должно
	}{
		{"compression below its version", "1.0", ` compression="zstd"`, "compression"},
		{"compression at its version", "1.2", ` compression="zstd"`, ""},
		{"compression above its version", "1.4", ` compression="zstd"`, ""},
		{"compact below its version", "1.0", ` compact="true"`, "compact"},
		{"compact at its version", "1.3.1", ` compact="true"`, ""},
		{"integrity below its version", "1.0", ` xxh3="deadbeef"`, "xxh3"},
		{"integrity at its version", "1.4", ` xxh3="deadbeef"`, ""},
		{"encryption below its version", "1.4", ` encryption="aes-256-gcm"`, "encryption"},
		{"encryption at its version", "1.5", ` encryption="aes-256-gcm"`, ""},
		{"plain packet, nothing to warn about", "1.0", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var err error
			out := captureLog(func() {
				_, err = NewParser().ParseBytes(packetXML(c.version, c.dataAttrs))
			})
			// Что бы ни было с версией, пакет обязан прочитаться.
			if err != nil {
				t.Fatalf("packet must be read regardless of the version mismatch, got: %v", err)
			}
			warned := strings.Contains(out, "WARNING")
			if c.wantWarn == "" {
				if warned {
					t.Errorf("unexpected warning: %q", out)
				}
				return
			}
			if !warned {
				t.Errorf("expected a warning about %s, got none", c.wantWarn)
			} else if !strings.Contains(out, c.wantWarn) {
				t.Errorf("warning does not mention %s: %q", c.wantWarn, out)
			}
		})
	}
}
