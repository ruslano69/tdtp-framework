package main

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

const sampleRegister = `<?xml version="1.0" encoding="UTF-8"?>
<REESTR_LN><RECORD>
<NUM_LN>123456-1234567890-1</NUM_LN><NUM_CASE>123456</NUM_CASE>
<VERSION>1</VERSION><RN_OKP>0012345678</RN_OKP>
<FIO>Іванов | Іван &amp; Олена</FIO>
<DATE_START>2026-09-01</DATE_START><DATE_END>2026-09-05</DATE_END>
<STATUS>Готово до сплати</STATUS>
</RECORD></REESTR_LN>`

func testOptions(t *testing.T, source string) options {
	t.Helper()
	dir := t.TempDir()
	in := filepath.Join(dir, "export.xml")
	if err := os.WriteFile(in, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return options{input: in, output: filepath.Join(dir, "leaves.tdtp.xml"), today: "2026-09-29", maxDays: 30, windowMonths: 1, windowField: "start"}
}

func TestConvertPFURegister(t *testing.T) {
	opt := testOptions(t, sampleRegister)
	if err := convert(opt); err != nil {
		t.Fatal(err)
	}
	pkt, err := packet.NewParser().ParseFile(opt.output)
	if err != nil {
		t.Fatal(err)
	}
	if err := packet.VerifyIntegrity(pkt); err != nil {
		t.Fatal(err)
	}
	if pkt.Version != "1.4" || pkt.Header.RecordsInPart != 1 || len(pkt.Data.Rows) != 1 {
		t.Fatalf("unexpected TDTP envelope: version=%s rows=%d", pkt.Version, len(pkt.Data.Rows))
	}
	values := packet.NewParser().GetRowValues(pkt.Data.Rows[0])
	if values[3] != "0012345678" || values[4] != "Іванов | Іван & Олена" || values[8] != "5" {
		t.Fatalf("wrong values: %#v", values)
	}
	if pkt.Schema.Fields[5].Type != "DATE" || pkt.Schema.Fields[8].Type != "INTEGER" {
		t.Fatalf("wrong schema: %+v", pkt.Schema.Fields)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(opt.output), "errors.xml")); !os.IsNotExist(err) {
		t.Fatalf("clean run produced error report: %v", err)
	}
}

func TestConvertPFURegisterWithMultipleRecords(t *testing.T) {
	second := `<RECORD>
<NUM_LN>987654-1234567890-2</NUM_LN><NUM_CASE>987654</NUM_CASE>
<VERSION>2</VERSION><RN_OKP>0098765432</RN_OKP>
<FIO>Петренко Петро</FIO>
<DATE_START>2026-09-10</DATE_START><DATE_END>2026-09-12</DATE_END>
<STATUS>Готово до сплати</STATUS>
</RECORD>`
	source := strings.Replace(sampleRegister, "</REESTR_LN>", second+"</REESTR_LN>", 1)
	opt := testOptions(t, source)
	if err := convert(opt); err != nil {
		t.Fatal(err)
	}
	pkt, err := packet.NewParser().ParseFile(opt.output)
	if err != nil {
		t.Fatal(err)
	}
	if err := packet.VerifyIntegrity(pkt); err != nil {
		t.Fatal(err)
	}
	if pkt.Header.RecordsInPart != 2 || len(pkt.Data.Rows) != 2 {
		t.Fatalf("expected two TDTP rows, got header=%d data=%d", pkt.Header.RecordsInPart, len(pkt.Data.Rows))
	}
	first := packet.NewParser().GetRowValues(pkt.Data.Rows[0])
	last := packet.NewParser().GetRowValues(pkt.Data.Rows[1])
	if first[0] != "123456-1234567890-1" || last[0] != "987654-1234567890-2" || last[8] != "3" {
		t.Fatalf("wrong record order or duration: first=%#v last=%#v", first, last)
	}
}

func TestConvertReportsInvalidRecords(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{"bad calendar date", strings.Replace(sampleRegister, "2026-09-05", "2026-02-31", 1), "invalid calendar date"},
		{"reversed dates", strings.Replace(sampleRegister, "2026-09-05", "2026-08-31", 1), "before DATE_START"},
		{"too long", strings.Replace(sampleRegister, "2026-09-05", "2026-10-05", 1), "exceeds --max-days"},
		{"outside window", strings.Replace(strings.Replace(sampleRegister, "2026-09-01", "2026-07-01", 1), "2026-09-05", "2026-07-05", 1), "outside"},
		{"missing identifier", strings.Replace(sampleRegister, "0012345678", "", 1), "RN_OKP is required"},
		{"duplicate field", strings.Replace(sampleRegister, "</VERSION>", "</VERSION><VERSION>2</VERSION>", 1), "duplicate field"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opt := testOptions(t, tc.input)
			if err := convert(opt); err == nil || !strings.Contains(err.Error(), "all 1 records failed validation") {
				t.Fatalf("expected all-invalid error, got %v", err)
			}
			if _, err := os.Stat(opt.output); !os.IsNotExist(err) {
				t.Fatalf("all-invalid input produced TDTP output: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(filepath.Dir(opt.output), "errors.xml"))
			if err != nil {
				t.Fatal(err)
			}
			var report validationReport
			if err := xml.Unmarshal(data, &report); err != nil {
				t.Fatal(err)
			}
			if report.TotalRecords != 1 || report.ValidRecords != 0 || report.FailedRecords != 1 || len(report.Errors) != 1 || !strings.Contains(report.Errors[0].Reason, tc.want) {
				t.Fatalf("unexpected report: %+v", report)
			}
		})
	}
}

func TestConvertRejectsInvalidEnvelopeWithoutOutput(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{"wrong shape", strings.ReplaceAll(sampleRegister, "REESTR_LN", "OTHER"), "REESTR_LN"},
		{"nested record", strings.Replace(sampleRegister, "</REESTR_LN>", "<WRAPPER><RECORD/></WRAPPER></REESTR_LN>", 1), "nested records are unsupported"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opt := testOptions(t, tc.input)
			err := convert(opt)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got error %v, want %q", err, tc.want)
			}
			if _, err := os.Stat(opt.output); !os.IsNotExist(err) {
				t.Fatalf("invalid envelope produced output: %v", err)
			}
		})
	}
}

func TestConvertKeepsValidRecordsWhenOneFails(t *testing.T) {
	invalid := strings.Replace(sampleRegister, "123456-1234567890-1", "987654-1234567890-2", 1)
	invalid = strings.Replace(invalid, "2026-09-05", "2026-02-31", 1)
	start := strings.Index(invalid, "<RECORD>")
	end := strings.Index(invalid, "</RECORD>") + len("</RECORD>")
	source := strings.Replace(sampleRegister, "</REESTR_LN>", invalid[start:end]+"</REESTR_LN>", 1)
	opt := testOptions(t, source)
	if err := convert(opt); err != nil {
		t.Fatal(err)
	}
	pkt, err := packet.NewParser().ParseFile(opt.output)
	if err != nil {
		t.Fatal(err)
	}
	if err := packet.VerifyIntegrity(pkt); err != nil {
		t.Fatal(err)
	}
	if pkt.Header.RecordsInPart != 1 || len(pkt.Data.Rows) != 1 {
		t.Fatalf("expected one valid TDTP row, got %d", len(pkt.Data.Rows))
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(opt.output), "errors.xml"))
	if err != nil {
		t.Fatal(err)
	}
	var report validationReport
	if err := xml.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.TotalRecords != 2 || report.ValidRecords != 1 || report.FailedRecords != 1 || report.Errors[0].Record != 2 || report.Errors[0].NumLN != "987654-1234567890-2" {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestConvertPreservesAdditionalTags(t *testing.T) {
	input := strings.Replace(sampleRegister, "</STATUS>", "</STATUS><EXTRA_CODE>007</EXTRA_CODE>", 1)
	opt := testOptions(t, input)
	if err := convert(opt); err != nil {
		t.Fatal(err)
	}
	pkt, err := packet.NewParser().ParseFile(opt.output)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Schema.Fields[8].Name != "EXTRA_CODE" || pkt.Schema.Fields[8].Type != "TEXT" {
		t.Fatalf("additional field missing: %+v", pkt.Schema.Fields)
	}
	values := packet.NewParser().GetRowValues(pkt.Data.Rows[0])
	if values[8] != "007" || values[9] != "5" {
		t.Fatalf("additional value lost: %#v", values)
	}
}

func TestCalendarMonthClamps(t *testing.T) {
	day, err := parseDate("2026-03-31")
	if err != nil {
		t.Fatal(err)
	}
	if got := shiftMonthClamped(day, -1).Format(time.DateOnly); got != "2026-02-28" {
		t.Fatalf("one calendar month before March 31 = %s", got)
	}
}

func TestConvertDoesNotOverwrite(t *testing.T) {
	opt := testOptions(t, sampleRegister)
	if err := os.WriteFile(opt.output, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := convert(opt); err == nil {
		t.Fatal("expected existing output to be refused")
	}
	data, err := os.ReadFile(opt.output)
	if err != nil || string(data) != "existing" {
		t.Fatalf("existing output changed: %q, %v", data, err)
	}
}

func TestConvertDoesNotOverwriteErrorReport(t *testing.T) {
	opt := testOptions(t, sampleRegister)
	opt.errors = filepath.Join(filepath.Dir(opt.output), "errors.xml")
	if err := os.WriteFile(opt.errors, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := convert(opt); err == nil || !strings.Contains(err.Error(), "error report already exists") {
		t.Fatalf("expected existing report to be refused, got %v", err)
	}
	data, err := os.ReadFile(opt.errors)
	if err != nil || string(data) != "existing" {
		t.Fatalf("existing report changed: %q, %v", data, err)
	}
	if _, err := os.Stat(opt.output); !os.IsNotExist(err) {
		t.Fatalf("TDTP output created despite report conflict: %v", err)
	}
}
