package validate

// validate.go — semantic pass of tdtp-validate plus the entry point.
//
// Structure (docs/tdtp.xsd conformance) runs first on the raw bytes
// (structure.go). Semantics runs on the parsed packet and checks what no
// XSD can express:
//
//   - every data row carries exactly len(Schema.Fields) values
//     (after decompression and compact/columnar expansion)
//   - Header.RecordsInPart matches the actual row count
//     (packet.VerifyRowCount — the counter is authoritative downstream)
//   - no duplicate field names; rows without a schema are rejected
//   - declared version covers the features used (compression → 1.2,
//     compact → 1.3.1, integrity → 1.4, encryption → 1.5). Producers stamp
//     this via packet.BumpVersion; archives predating the convention fail
//     here with a message saying exactly that — use an older tdtpcli
//     --test if you only need those read, not judged.
//   - v1.4 three-level xxh3 hashes verify (packet.VerifyIntegrity);
//     a partial stamp (e.g. Data xxh3 without the packet fingerprint)
//     is malformed on its own
//   - QueryContext counters are internally consistent
//     (Returned == rows in this part, AfterFilters >= Returned,
//     Total >= AfterFilters; multipart responses are exempt from the
//     Returned check — per-part semantics there are the writer's business)
//
// Encrypted sections (v1.5) are opaque: structure is checked, content is
// reported, not judged. Pass order matters — decompress, verify integrity
// on the raw rows, then expand compact/columnar for the shape checks —
// because the hashes cover the raw values, not the expanded ones.

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// Report is the validation verdict for one file.
type Report struct {
	File     string   `json:"file"`
	Version  string   `json:"version"`
	PktType  string   `json:"packet_type"`
	Table    string   `json:"table"`
	Rows     int      `json:"rows"`
	Cols     int      `json:"cols"`
	Features []string `json:"features,omitempty"`
	Notes    []string `json:"notes,omitempty"`
	Errors   []string `json:"errors,omitempty"`
}

// Valid reports whether the file conforms.
func (r Report) Valid() bool { return len(r.Errors) == 0 }

// versionIntroduced mirrors packet.versionIntroduced (same ground truth,
// duplicated because that table is unexported): feature → spec version.
var versionIntroduced = []struct {
	feature string
	since   string
	present func(*packet.DataPacket) bool
}{
	{"compression", "1.2", func(p *packet.DataPacket) bool { return p.Data.Compression != "" }},
	{"compact format", "1.3.1", func(p *packet.DataPacket) bool { return p.Data.Compact }},
	{"xxh3 integrity", "1.4", func(p *packet.DataPacket) bool {
		return p.XXH3 != "" || p.Schema.XXH3 != "" || p.Data.XXH3 != ""
	}},
	{"section encryption", "1.5", func(p *packet.DataPacket) bool {
		return p.Schema.Encryption != "" || p.Data.Encryption != "" ||
			(p.QueryContext != nil && p.QueryContext.Encryption != "")
	}},
}

// Validate checks one TDTP file's contents and returns the verdict.
// IO errors come back as Go errors; spec violations land in Report.Errors.
func Validate(data []byte, filename string) Report {
	return validate(data, filename, false)
}

// validate is Validate with a version-coverage blind spot for mutation
// pre-checks: stamping or stripping integrity is exactly what brings an
// old file's version back in line, so the version-predates error must not
// block the operation that fixes it. Every other violation still blocks.
func validate(data []byte, filename string, skipVersionCheck bool) Report {
	rep := Report{File: filename}

	structErrs := checkStructure(data)
	rep.Errors = append(rep.Errors, structErrs...)
	if len(structErrs) > 0 {
		// Fail fast by layer: semantic checks read the parsed packet,
		// whose shape is untrustworthy once structure is broken.
		// Reporting both layers at once produced cascades (e.g. a
		// garbage xxh3 attribute tripping integrity verification noise
		// on top of the real pattern error).
		return rep
	}

	pkt, err := packet.NewParser().ParseBytes(data)
	if err != nil {
		rep.Errors = append(rep.Errors, "semantics: document: unparsable as TDTP packet: "+err.Error())
		return rep
	}

	rep.Version = pkt.Version
	rep.PktType = string(pkt.Header.Type)
	rep.Table = pkt.Header.TableName
	rep.Cols = len(pkt.Schema.Fields)
	rep.Features = packetFeatures(pkt)

	sem := &semchecker{skipVersion: skipVersionCheck}
	checkSemantics(sem, pkt)
	rep.Errors = append(rep.Errors, sem.errs...)
	rep.Rows = sem.rows
	return rep
}

// packetFeatures lists the wire features in use, for the report header.
func packetFeatures(pkt *packet.DataPacket) []string {
	var f []string
	if pkt.Data.Compression != "" {
		f = append(f, "compressed("+pkt.Data.Compression+")")
	}
	if pkt.Data.Compact {
		f = append(f, "compact")
	}
	if pkt.Data.Layout == packet.LayoutColumns {
		f = append(f, "columnar")
	}
	if packet.HasIntegrity(pkt) {
		f = append(f, "integrity")
	}
	if pkt.Schema.Encryption != "" || pkt.Data.Encryption != "" ||
		(pkt.QueryContext != nil && pkt.QueryContext.Encryption != "") {
		f = append(f, "encrypted")
	}
	if pkt.Query != nil {
		f = append(f, "query")
	}
	if pkt.QueryContext != nil {
		f = append(f, "query-context")
	}
	if pkt.PipelineContext != nil {
		f = append(f, "pipeline-context")
	}
	if pkt.Header.Type == packet.TypeAlarm || pkt.AlarmDetails != nil {
		f = append(f, "alarm")
	}
	return f
}

// semchecker carries semantic errors, the resolved row count, and the
// version-check blind spot for mutation pre-checks (see validate).
type semchecker struct {
	errs []string
	rows int
	// skipVersion silences the version-predates error: the mutation itself
	// restores the version, so that error must not block it.
	skipVersion bool
}

func (c *semchecker) serrf(format string, args ...any) {
	c.errs = append(c.errs, "semantics: "+fmt.Sprintf(format, args...))
}

func checkSemantics(c *semchecker, pkt *packet.DataPacket) {
	if !c.skipVersion {
		checkVersionCoversFeatures(c, pkt)
	}
	checkFieldNames(c, pkt)
	checkHeader(c, pkt)
	checkDictionary(c, pkt)
	checkChecksumWithoutCompression(c, pkt)

	encryptedData := pkt.Data.Encryption != ""
	encryptedSchema := pkt.Schema.Encryption != ""

	// Rows are opaque while compressed or encrypted: decompress first
	// (validates the checksum), integrity second on the RAW rows.
	if !encryptedData && pkt.Data.Compression != "" {
		if err := processors.DecompressPacket(context.Background(), pkt); err != nil {
			c.serrf("Data: decompression failed: %v", err)
			return
		}
	}

	if packet.HasIntegrity(pkt) {
		if err := packet.VerifyIntegrity(pkt); err != nil {
			c.serrf("integrity: %v", err)
		}
	} else if pkt.Schema.XXH3 != "" || pkt.Data.XXH3 != "" {
		c.serrf("integrity: partial stamp (Schema/Data xxh3 without the packet fingerprint) — stamp all three levels or none")
	}

	if encryptedData {
		c.rows = len(pkt.Data.Rows) // the single opaque row, for the report
		return                      // content is ciphertext: nothing further to judge
	}

	// Shape checks run on the expanded view, in reverse production order.
	// The export chain lays out compact BEFORE columnar (Compact →
	// Integrity → Columnar → Compress), so decoding undoes columnar first:
	// compact rows legitimately carry fewer cells, columnar <R>s are
	// columns, not rows. Both expanders are no-ops when their flag is
	// absent (and DecompressPacket above already expanded columnar data
	// that arrived compressed), so unconditional calls are safe.
	if pkt.Data.Layout == packet.LayoutColumns {
		if err := packet.ExpandColumnarRows(pkt); err != nil {
			c.serrf("Data: columnar expansion failed: %v", err)
			return
		}
	}
	if pkt.Data.Compact && !encryptedSchema {
		if err := packet.ExpandCompactRows(pkt); err != nil {
			c.serrf("Data: compact expansion failed: %v", err)
			return
		}
	}

	if err := packet.VerifyRowCount(pkt); err != nil {
		c.serrf("%v", err)
	}

	nFields := len(pkt.Schema.Fields)
	rows := pkt.GetRows()
	c.rows = len(rows)
	if nFields == 0 {
		if len(rows) > 0 {
			c.serrf("Data: %d row(s) with an empty Schema — values are uninterpretable", len(rows))
		}
	} else {
		for i, r := range rows {
			if len(r) != nFields {
				c.serrf("Data / R[%d]: %d value(s), Schema declares %d field(s)", i+1, len(r), nFields)
			}
		}
	}

	checkQueryContext(c, pkt, len(rows))
}

// checkVersionCoversFeatures enforces the versionIntroduced table: a packet
// using a feature must declare at least the version that introduced it.
// Producers stamp this via packet.BumpVersion at the step that applies the
// feature; archives written before that convention fail here BY DESIGN —
// the message says so, pointing at --test for read-only checking instead.
func checkVersionCoversFeatures(c *semchecker, pkt *packet.DataPacket) {
	declared, ok := packet.ParseProtocolVersion(pkt.Version)
	if !ok {
		c.serrf("DataPacket: version %q is not a dotted version", pkt.Version)
		return
	}
	for _, f := range versionIntroduced {
		if !f.present(pkt) {
			continue
		}
		since, _ := packet.ParseProtocolVersion(f.since)
		if declared.Compare(since) < 0 {
			c.serrf("DataPacket: version %q predates %s (introduced in %s) — "+
				"re-export with a current producer to stamp it; pre-convention archives predate this rule",
				pkt.Version, f.feature, f.since)
		}
	}
}

// checkHeader enforces what neither the XSD nor the parser constrains: a
// negative RecordsInPart. (Empty MessageID and PartNumber > TotalParts are
// already rejected by packet.Parser itself — the validator reports those
// through the parse error, not from here.)
func checkHeader(c *semchecker, pkt *packet.DataPacket) {
	if pkt.Header.RecordsInPart < 0 {
		c.serrf("Header: RecordsInPart must be >= 0, got %d", pkt.Header.RecordsInPart)
	}
}

// checkDictionary reuses the producer-side validator: abbreviation tokens
// have a strict whole-cell grammar, and a malformed Dictionary poisons
// every consumer that expands it.
func checkDictionary(c *semchecker, pkt *packet.DataPacket) {
	if pkt.Schema.Dictionary == nil {
		return
	}
	if err := packet.ValidateDictionary(pkt.Schema.Dictionary.Entries); err != nil {
		c.serrf("Schema: invalid Dictionary: %v", err)
	}
}

// checkChecksumWithoutCompression rejects a checksum with nothing to check:
// Data.Checksum is defined as the xxh3-64 of the COMPRESSED bytes, so on an
// uncompressed packet it is either stale or meaningless.
func checkChecksumWithoutCompression(c *semchecker, pkt *packet.DataPacket) {
	if pkt.Data.Checksum != "" && pkt.Data.Compression == "" {
		c.serrf("Data: checksum present without compression — checksum covers the compressed bytes")
	}
}

// checkFieldNames rejects duplicate field names: downstream projection
// (--fields) and row assembly address columns by name.
func checkFieldNames(c *semchecker, pkt *packet.DataPacket) {
	seen := map[string]int{}
	for i, f := range pkt.Schema.Fields {
		if f.Name == "" {
			c.serrf("Schema / Field[%d]: empty field name", i+1)
			continue
		}
		if prev, dup := seen[f.Name]; dup {
			c.serrf("Schema: duplicate field name %q (Field[%d] and Field[%d])", f.Name, prev, i+1)
		} else {
			seen[f.Name] = i + 1
		}
	}
}

// checkQueryContext validates the response counters against each other and
// against the actual payload. Multipart responses skip the Returned check:
// how a part relates to the whole is the writer's contract, not the spec's.
func checkQueryContext(c *semchecker, pkt *packet.DataPacket, nrows int) {
	qc := pkt.QueryContext
	if qc == nil || qc.Encryption != "" {
		return
	}
	er := qc.ExecutionResults
	if er.TotalRecordsInTable < er.RecordsAfterFilters {
		c.serrf("QueryContext: TotalRecordsInTable (%d) < RecordsAfterFilters (%d)",
			er.TotalRecordsInTable, er.RecordsAfterFilters)
	}
	if er.RecordsAfterFilters < er.RecordsReturned {
		c.serrf("QueryContext: RecordsAfterFilters (%d) < RecordsReturned (%d)",
			er.RecordsAfterFilters, er.RecordsReturned)
	}
	if pkt.Header.TotalParts <= 1 && er.RecordsReturned != nrows {
		c.serrf("QueryContext: RecordsReturned (%d) != rows in this packet (%d)",
			er.RecordsReturned, nrows)
	}
	checkOriginalQueryFields(c, pkt)
}

// checkOriginalQueryFields verifies that the response's OriginalQuery only
// names columns the response Schema actually declares. A filter or sort on
// a ghost column means the query and the payload disagree about the table.
// Request packets (Type=request) are exempt: a request legitimately carries
// a Query against a Schema the responder has yet to fill in.
func checkOriginalQueryFields(c *semchecker, pkt *packet.DataPacket) {
	qc := pkt.QueryContext
	if qc == nil || qc.Encryption != "" {
		return
	}
	known := map[string]bool{}
	for _, f := range pkt.Schema.Fields {
		known[f.Name] = true
	}
	var walkGroup func(g *packet.LogicalGroup)
	walkGroup = func(g *packet.LogicalGroup) {
		for _, f := range g.Filters {
			if !known[f.Field] {
				c.serrf("QueryContext: OriginalQuery filters unknown field %q", f.Field)
			}
		}
		for i := range g.And {
			walkGroup(&g.And[i])
		}
		for i := range g.Or {
			walkGroup(&g.Or[i])
		}
	}
	q := qc.OriginalQuery
	if q.Filters != nil {
		if q.Filters.And != nil {
			walkGroup(q.Filters.And)
		}
		if q.Filters.Or != nil {
			walkGroup(q.Filters.Or)
		}
	}
	if q.OrderBy != nil {
		if q.OrderBy.Field != "" && !known[q.OrderBy.Field] {
			c.serrf("QueryContext: OriginalQuery sorts unknown field %q", q.OrderBy.Field)
		}
		for _, f := range q.OrderBy.Fields {
			if !known[f.Name] {
				c.serrf("QueryContext: OriginalQuery sorts unknown field %q", f.Name)
			}
		}
	}
	for _, f := range q.Fields {
		if !known[f] {
			c.serrf("QueryContext: OriginalQuery projects unknown field %q", f)
		}
	}
}

// Format renders the human-readable report.
func (r Report) Format() string {
	var b strings.Builder
	feat := ""
	if len(r.Features) > 0 {
		feat = " [" + strings.Join(r.Features, ", ") + "]"
	}
	fmt.Fprintf(&b, "%s: %s table=%q rows=%d cols=%d%s\n",
		r.File, r.Version, r.Table, r.Rows, r.Cols, feat)
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "  ~ %s\n", n)
	}
	if r.Valid() {
		b.WriteString("  VALID\n")
		return b.String()
	}
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "  x %s\n", e)
	}
	fmt.Fprintf(&b, "  INVALID: %d error(s)\n", len(r.Errors))
	return b.String()
}
