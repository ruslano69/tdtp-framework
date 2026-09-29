// Run with: go run ./examples/pfu-xml --input export.xml --output leaves.tdtp.xml --max-days 30
package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// This converter accepts the illustrated REESTR_LN/RECORD shape only. The
// actual PFU export.xml must be checked before its field mapping is added.
type xmlRecord struct {
	XMLName xml.Name
	Fields  []xmlField `xml:",any"`
}

type xmlField struct {
	XMLName  xml.Name
	Value    string     `xml:",chardata"`
	Children []xmlField `xml:",any"`
}

type xmlRegister struct {
	XMLName xml.Name    `xml:"REESTR_LN"`
	Records []xmlRecord `xml:"RECORD"`
}

type options struct {
	input        string
	output       string
	errors       string
	today        string
	maxDays      int
	windowMonths int
	windowField  string
}

type parsedRecord struct {
	values   map[string]string
	duration int
}

type validationReport struct {
	XMLName       xml.Name          `xml:"ValidationReport"`
	TotalRecords  int               `xml:"total_records,attr"`
	ValidRecords  int               `xml:"valid_records,attr"`
	FailedRecords int               `xml:"failed_records,attr"`
	Errors        []validationError `xml:"Error"`
}

type validationError struct {
	Record int    `xml:"record,attr"`
	NumLN  string `xml:"num_ln,attr,omitempty"`
	Reason string `xml:",chardata"`
}

var sourceFields = []packet.Field{
	{Name: "NUM_LN", Type: "TEXT", Key: true},
	{Name: "NUM_CASE", Type: "TEXT"},
	{Name: "VERSION", Type: "INTEGER"},
	{Name: "RN_OKP", Type: "TEXT"},
	{Name: "FIO", Type: "TEXT"},
	{Name: "DATE_START", Type: "DATE"},
	{Name: "DATE_END", Type: "DATE"},
	{Name: "STATUS", Type: "TEXT"},
	{Name: "DURATION_DAYS", Type: "INTEGER"},
}

func main() {
	var opt options
	flag.StringVar(&opt.input, "input", "", "PFU XML input file")
	flag.StringVar(&opt.output, "output", "", "TDTP XML output file")
	flag.StringVar(&opt.errors, "errors", "", "validation report path (default: errors.xml beside output)")
	flag.StringVar(&opt.today, "today", "", "reference date YYYY-MM-DD (default: today in Europe/Kyiv)")
	flag.IntVar(&opt.maxDays, "max-days", 0, "maximum inclusive duration (required)")
	flag.IntVar(&opt.windowMonths, "window-months", 1, "calendar months before/after reference date")
	flag.StringVar(&opt.windowField, "window-field", "start", "date checked against window: start, end, or both")
	flag.Parse()
	if flag.NArg() != 0 {
		fail(fmt.Errorf("unexpected arguments: %v", flag.Args()))
	}
	if err := convert(opt); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "pfu_xml_to_tdtp:", err)
	os.Exit(1)
}

func convert(opt options) error {
	if opt.input == "" || opt.output == "" || opt.maxDays <= 0 {
		return errors.New("--input, --output and positive --max-days are required")
	}
	if opt.windowMonths < 0 || (opt.windowField != "start" && opt.windowField != "end" && opt.windowField != "both") {
		return errors.New("--window-months must be nonnegative; --window-field must be start, end, or both")
	}
	inAbs, err := filepath.Abs(opt.input)
	if err != nil {
		return err
	}
	outAbs, err := filepath.Abs(opt.output)
	if err != nil {
		return err
	}
	if strings.EqualFold(inAbs, outAbs) {
		return errors.New("input and output must be different files")
	}
	if opt.errors == "" {
		opt.errors = filepath.Join(filepath.Dir(opt.output), "errors.xml")
	}
	errorsAbs, err := filepath.Abs(opt.errors)
	if err != nil {
		return err
	}
	if strings.EqualFold(errorsAbs, inAbs) || strings.EqualFold(errorsAbs, outAbs) {
		return errors.New("error report must differ from input and output")
	}
	if _, err := os.Stat(opt.output); err == nil {
		return fmt.Errorf("output already exists: %s", opt.output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(opt.errors); err == nil {
		return fmt.Errorf("error report already exists: %s", opt.errors)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	location, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		return fmt.Errorf("load Kyiv time zone: %w", err)
	}
	reference := time.Now().In(location)
	if opt.today != "" {
		reference, err = parseDate(opt.today)
		if err != nil {
			return fmt.Errorf("--today: %w", err)
		}
	}
	// Compare calendar dates in UTC, so daylight-saving transitions do not
	// change the number of days in a sick-leave interval.
	reference = time.Date(reference.Year(), reference.Month(), reference.Day(), 0, 0, 0, 0, time.UTC)

	data, err := os.ReadFile(opt.input)
	if err != nil {
		return err
	}
	if bytes.Contains(bytes.ToUpper(data), []byte("<!DOCTYPE")) {
		return errors.New("DOCTYPE is not accepted in the source XML")
	}
	var source xmlRegister
	if err := xml.Unmarshal(data, &source); err != nil {
		return fmt.Errorf("parse source XML: %w", err)
	}
	if source.XMLName.Local != "REESTR_LN" || len(source.Records) == 0 {
		return errors.New("expected REESTR_LN with at least one RECORD; check the actual PFU export format")
	}
	count, err := countRecordElements(data)
	if err != nil {
		return err
	}
	if count != len(source.Records) {
		return fmt.Errorf("found %d RECORD elements but only %d direct REESTR_LN/RECORD entries; nested records are unsupported", count, len(source.Records))
	}

	parsedRows := make([]parsedRecord, 0, len(source.Records))
	schemaFields := append([]packet.Field(nil), sourceFields[:len(sourceFields)-1]...)
	knownFields := make(map[string]bool, len(sourceFields))
	for _, field := range sourceFields {
		knownFields[field.Name] = true
	}
	var issues []validationError
	for i, record := range source.Records {
		parsed, err := parseRecord(record, reference, opt)
		if err != nil {
			issue := validationError{Record: i + 1, Reason: err.Error()}
			for _, field := range record.Fields {
				if field.XMLName.Local == "NUM_LN" {
					issue.NumLN = strings.TrimSpace(field.Value)
					break
				}
			}
			issues = append(issues, issue)
			continue
		}
		for _, field := range record.Fields {
			name := field.XMLName.Local
			if !knownFields[name] {
				schemaFields = append(schemaFields, packet.Field{Name: name, Type: "TEXT"})
				knownFields[name] = true
			}
		}
		parsedRows = append(parsedRows, parsed)
	}
	if len(parsedRows) == 0 {
		report := validationReport{
			TotalRecords: len(source.Records), FailedRecords: len(issues), Errors: issues,
		}
		if err := writeErrorsAtomic(opt.errors, report); err != nil {
			return fmt.Errorf("write error report: %w", err)
		}
		return fmt.Errorf("all %d records failed validation; details: %s", len(issues), opt.errors)
	}
	schemaFields = append(schemaFields, sourceFields[len(sourceFields)-1])

	pkt := packet.NewDataPacket(packet.TypeReference, "pfu_sick_leaves")
	pkt.Version = "1.4"
	pkt.Header.RecordsInPart = len(parsedRows)
	pkt.Header.Sender = "pfu_xml_to_tdtp"
	pkt.Schema.Fields = schemaFields
	pkt.Data.Rows = make([]packet.Row, len(parsedRows))
	for i, parsed := range parsedRows {
		row := make([]string, len(schemaFields))
		for j, field := range schemaFields[:len(schemaFields)-1] {
			row[j] = parsed.values[field.Name]
		}
		row[len(row)-1] = strconv.Itoa(parsed.duration)
		pkt.Data.Rows[i] = packet.Row{Value: packet.JoinRowEscaped(row)}
	}
	pkt.PipelineContext = &packet.PipelineContext{
		Pipeline: packet.PipelineInfo{Name: "pfu_xml_to_tdtp", Version: "1.0"},
		Variables: []packet.PipelineVar{
			{Name: "reference_date", Value: reference.Format("2006-01-02")},
			{Name: "window_months", Value: strconv.Itoa(opt.windowMonths)},
			{Name: "window_field", Value: opt.windowField},
			{Name: "max_days", Value: strconv.Itoa(opt.maxDays)},
		},
	}
	if _, err := packet.ComputeIntegrity(pkt); err != nil {
		return fmt.Errorf("compute TDTP integrity: %w", err)
	}
	if len(issues) > 0 {
		report := validationReport{
			TotalRecords: len(source.Records), ValidRecords: len(parsedRows),
			FailedRecords: len(issues), Errors: issues,
		}
		if err := writeErrorsAtomic(opt.errors, report); err != nil {
			return fmt.Errorf("write error report: %w", err)
		}
	}
	if err := writeAtomic(opt.output, pkt); err != nil {
		if len(issues) > 0 {
			_ = os.Remove(opt.errors)
		}
		return err
	}
	fmt.Printf("Converted %d records; reference date %s; output %s\n", len(parsedRows), reference.Format("2006-01-02"), opt.output)
	if len(issues) > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: skipped %d invalid records; details: %s\n", len(issues), opt.errors)
	}
	return nil
}

func writeErrorsAtomic(path string, report validationReport) error {
	data, err := xml.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".pfu-errors-*.xml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(append([]byte(xml.Header), data...)); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("error report already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(tmpPath, path)
}

func countRecordElements(data []byte) (int, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	count := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return count, nil
		}
		if err != nil {
			return 0, err
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "RECORD" {
			count++
		}
	}
}

func parseRecord(record xmlRecord, reference time.Time, opt options) (parsedRecord, error) {
	values := make(map[string]string, len(record.Fields))
	for _, field := range record.Fields {
		name := field.XMLName.Local
		if name == "DURATION_DAYS" {
			return parsedRecord{}, errors.New("DURATION_DAYS is reserved for the calculated duration")
		}
		if len(field.Children) > 0 {
			return parsedRecord{}, fmt.Errorf("%s contains nested XML; only flat fields are supported", name)
		}
		if _, exists := values[name]; exists {
			return parsedRecord{}, fmt.Errorf("duplicate field %s", name)
		}
		values[name] = strings.TrimSpace(field.Value)
	}
	for _, name := range []string{"NUM_LN", "NUM_CASE", "VERSION", "RN_OKP", "FIO", "DATE_START", "DATE_END", "STATUS"} {
		if values[name] == "" {
			return parsedRecord{}, fmt.Errorf("%s is required", name)
		}
	}
	version, err := strconv.Atoi(values["VERSION"])
	if err != nil || version < 1 {
		return parsedRecord{}, errors.New("VERSION must be a positive integer")
	}
	start, err := parseDate(values["DATE_START"])
	if err != nil {
		return parsedRecord{}, fmt.Errorf("DATE_START: %w", err)
	}
	end, err := parseDate(values["DATE_END"])
	if err != nil {
		return parsedRecord{}, fmt.Errorf("DATE_END: %w", err)
	}
	if end.Before(start) {
		return parsedRecord{}, errors.New("DATE_END is before DATE_START")
	}
	duration := int(end.Sub(start).Hours()/24) + 1
	if duration > opt.maxDays {
		return parsedRecord{}, fmt.Errorf("duration %d days exceeds --max-days %d", duration, opt.maxDays)
	}
	lower := shiftMonthClamped(reference, -opt.windowMonths)
	upper := shiftMonthClamped(reference, opt.windowMonths)
	check := func(name string, date time.Time) error {
		if date.Before(lower) || date.After(upper) {
			return fmt.Errorf("%s outside %s..%s", name, lower.Format("2006-01-02"), upper.Format("2006-01-02"))
		}
		return nil
	}
	if opt.windowField == "start" || opt.windowField == "both" {
		if err := check("DATE_START", start); err != nil {
			return parsedRecord{}, err
		}
	}
	if opt.windowField == "end" || opt.windowField == "both" {
		if err := check("DATE_END", end); err != nil {
			return parsedRecord{}, err
		}
	}
	return parsedRecord{values: values, duration: duration}, nil
}

func parseDate(value string) (time.Time, error) {
	date, err := time.Parse("2006-01-02", value)
	if err != nil || date.Format("2006-01-02") != value {
		return time.Time{}, fmt.Errorf("invalid calendar date %q; expected YYYY-MM-DD", value)
	}
	return date, nil
}

// Clamp the day at the target month's end (31 March - 1 month = 28/29 February).
func shiftMonthClamped(date time.Time, months int) time.Time {
	first := time.Date(date.Year(), date.Month()+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	lastDay := first.AddDate(0, 1, -1).Day()
	day := date.Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, time.UTC)
}

func writeAtomic(path string, pkt *packet.DataPacket) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".pfu-*.tdtp.xml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)
	if err := packet.NewGenerator().WriteToFile(pkt, tmpPath); err != nil {
		return err
	}
	parsed, err := packet.NewParser().ParseFile(tmpPath)
	if err != nil {
		return fmt.Errorf("verify written TDTP: %w", err)
	}
	if err := packet.VerifyIntegrity(parsed); err != nil {
		return fmt.Errorf("verify written TDTP integrity: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("output already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(tmpPath, path)
}
