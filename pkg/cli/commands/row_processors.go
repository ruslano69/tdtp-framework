package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/cliconfig"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
	"gopkg.in/yaml.v3"
)

// RowProcessors is the --mask/--validate/--normalize chain both CLIs build
// from flags (v1 through its ProcessorManager alias). It satisfies the
// ProcessorManager interface the engines take.
type RowProcessors struct {
	chain *processors.Chain
}

// NewRowProcessors creates a new processor manager
func NewRowProcessors() *RowProcessors {
	return &RowProcessors{
		chain: processors.NewChain(),
	}
}

// AddMaskProcessor adds field masking processor from CLI flag
// Format: --mask email,phone,card
func (pm *RowProcessors) AddMaskProcessor(maskFields string) error { //nolint:unparam // error return kept for API consistency
	if maskFields == "" {
		return nil
	}

	fields := strings.Split(maskFields, ",")
	fieldsToMask := make(map[string]processors.MaskPattern)

	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}

		// Определяем паттерн маскирования по имени поля
		pattern := detectMaskPattern(field)
		fieldsToMask[field] = pattern
	}

	if len(fieldsToMask) > 0 {
		masker := processors.NewFieldMasker(fieldsToMask)
		pm.chain.Add(masker)
		if !QuietOutput() {
			fmt.Printf("✓ Added field masker: %d field(s)\n", len(fieldsToMask))
		}
	}

	return nil
}

// AddValidateProcessor adds field validation processor from YAML file.
// Format: --validate rules.yaml
//
// YAML structure:
//
//	rules:
//	  email: email
//	  age: range:0-150
//	  status: [required, "enum:active,inactive"]
//	on_error: fail          # fail (default) | filter | warn
//	stop_on_first_error: false   # optional, only for on_error: fail
//
// on_error strategies:
//   - fail   — abort on errors, return full error list
//   - filter — remove invalid rows, pass the rest (count printed to stderr)
//   - warn   — print warnings to stderr, pass all rows unchanged
//
// A file without a "rules" section is an error. It used to skip silently:
// --validate naming the wrong file validated nothing and said so nowhere.
func (pm *RowProcessors) AddValidateProcessor(rulesFile string) error {
	if rulesFile == "" {
		return nil
	}

	data, err := os.ReadFile(rulesFile)
	if err != nil {
		return fmt.Errorf("failed to read validate rules file %q: %w", rulesFile, err)
	}

	var params map[string]any
	if err := yaml.Unmarshal(data, &params); err != nil {
		return fmt.Errorf("failed to parse validate rules file %q: %w", rulesFile, err)
	}

	if _, ok := params["rules"]; !ok {
		return fmt.Errorf("validate rules file %q has no \"rules:\" section — nothing would be validated", rulesFile)
	}

	validator, err := processors.NewFieldValidatorFromConfig(params)
	if err != nil {
		return fmt.Errorf("failed to create validator from %q: %w", rulesFile, err)
	}

	pm.chain.Add(validator)
	if !QuietOutput() {
		fmt.Printf("✓ Added field validator from: %s\n", rulesFile)
	}

	return nil
}

// AddNormalizeProcessor adds field normalization processor from YAML file.
// Format: --normalize rules.yaml
//
// YAML structure:
//
//	fields:
//	  email: email
//	  phone: phone
//	  city: uppercase
//
// Supported rules: email, phone, whitespace, uppercase, lowercase, date.
// A file without a "fields" section is an error, for the same reason as in
// AddValidateProcessor.
func (pm *RowProcessors) AddNormalizeProcessor(rulesFile string) error {
	if rulesFile == "" {
		return nil
	}

	data, err := os.ReadFile(rulesFile)
	if err != nil {
		return fmt.Errorf("failed to read normalize rules file %q: %w", rulesFile, err)
	}

	var params map[string]any
	if err := yaml.Unmarshal(data, &params); err != nil {
		return fmt.Errorf("failed to parse normalize rules file %q: %w", rulesFile, err)
	}

	if _, ok := params["fields"]; !ok {
		return fmt.Errorf("normalize rules file %q has no \"fields:\" section — nothing would be normalized", rulesFile)
	}

	normalizer, err := processors.NewFieldNormalizerFromConfig(params)
	if err != nil {
		return fmt.Errorf("failed to create normalizer from %q: %w", rulesFile, err)
	}

	pm.chain.Add(normalizer)
	if !QuietOutput() {
		fmt.Printf("✓ Added field normalizer from: %s\n", rulesFile)
	}

	return nil
}

// AddMaskRules builds a masker from config-file rules (cliconfig
// ProcessorsConfig.Mask): - mask: [{field: email, strategy: partial}].
// A strategy naming a valid MaskPattern is honored; anything else falls
// back to name-based auto-detection — the same rule --mask applies to
// bare field names.
func (pm *RowProcessors) AddMaskRules(rules []cliconfig.MaskRule) error {
	if len(rules) == 0 {
		return nil
	}
	fieldsToMask := make(map[string]processors.MaskPattern, len(rules))
	for _, r := range rules {
		field := strings.TrimSpace(r.Field)
		if field == "" {
			continue
		}
		pattern := processors.MaskPattern(strings.TrimSpace(r.Strategy))
		switch pattern {
		case processors.MaskPartial, processors.MaskMiddle,
			processors.MaskStars, processors.MaskFirst2Last2:
			// honored as written
		default:
			pattern = detectMaskPattern(field)
		}
		fieldsToMask[field] = pattern
	}
	if len(fieldsToMask) == 0 {
		return nil
	}
	pm.chain.Add(processors.NewFieldMasker(fieldsToMask))
	if !QuietOutput() {
		fmt.Printf("✓ Added field masker from config: %d field(s)\n", len(fieldsToMask))
	}
	return nil
}

// AddValidateRules builds a validator from config-file rules:
// - validate: [{field: age, type: range, min: "0", max: "150"}].
// Type must name a known rule (regex, range, enum, required, length,
// email, phone, url, date); the param is Pattern, else Min-Max joined
// exactly as the rule-file grammar spells it ("range:0-150"). Anything
// else is a usage error, like a bad rule file.
func (pm *RowProcessors) AddValidateRules(rules []cliconfig.ValidateRule) error {
	if len(rules) == 0 {
		return nil
	}
	fieldsToValidate := make(map[string][]processors.FieldValidationRule, len(rules))
	for _, r := range rules {
		field := strings.TrimSpace(r.Field)
		if field == "" {
			continue
		}
		typ := processors.ValidationRule(strings.TrimSpace(r.Type))
		switch typ {
		case processors.ValidateRegex, processors.ValidateRange,
			processors.ValidateEnum, processors.ValidateRequired,
			processors.ValidateLength, processors.ValidateEmail,
			processors.ValidatePhone, processors.ValidateURL,
			processors.ValidateDate:
			// known type, same set the rule-file grammar accepts
		default:
			return fmt.Errorf("invalid validate rule for field %q: unknown type %q", field, r.Type)
		}
		param := strings.TrimSpace(r.Pattern)
		if param == "" && (strings.TrimSpace(r.Min) != "" || strings.TrimSpace(r.Max) != "") {
			param = strings.TrimSpace(r.Min) + "-" + strings.TrimSpace(r.Max)
		}
		fieldsToValidate[field] = append(fieldsToValidate[field], processors.FieldValidationRule{
			Type:  typ,
			Param: param,
		})
	}
	if len(fieldsToValidate) == 0 {
		return nil
	}
	validator, err := processors.NewFieldValidator(fieldsToValidate, false)
	if err != nil {
		return fmt.Errorf("failed to create validator from config: %w", err)
	}
	pm.chain.Add(validator)
	if !QuietOutput() {
		fmt.Printf("✓ Added field validator from config: %d field(s)\n", len(fieldsToValidate))
	}
	return nil
}

// AddNormalizeRules builds a normalizer from config-file rules:
// - normalize: [{field: city, strategy: uppercase}].
// Unknown strategies fail at build, like a bad rule file.
func (pm *RowProcessors) AddNormalizeRules(rules []cliconfig.NormalizeRule) error {
	if len(rules) == 0 {
		return nil
	}
	fieldsToNormalize := make(map[string]processors.NormalizeRule, len(rules))
	for _, r := range rules {
		field := strings.TrimSpace(r.Field)
		if field == "" {
			continue
		}
		fieldsToNormalize[field] = processors.NormalizeRule(strings.TrimSpace(r.Strategy))
	}
	if len(fieldsToNormalize) == 0 {
		return nil
	}
	pm.chain.Add(processors.NewFieldNormalizer(fieldsToNormalize))
	if !QuietOutput() {
		fmt.Printf("✓ Added field normalizer from config: %d field(s)\n", len(fieldsToNormalize))
	}
	return nil
}

// Name implements processors.PacketProcessor.
func (pm *RowProcessors) Name() string { return "row-chain" }

// ProcessPacket applies all processors to a packet's data
func (pm *RowProcessors) ProcessPacket(ctx context.Context, pkt *packet.DataPacket) error {
	if pm.chain.IsEmpty() {
		return nil
	}

	// Материализуем rawRows (GenerateReference fast-path) — иначе Data.Rows пуст
	// и mask/normalize/validate молча пропускаются.
	pkt.MaterializeRows()

	processed, err := pm.chain.Process(ctx, packetToMatrix(pkt), pkt.Schema)
	if err != nil {
		return fmt.Errorf("processor chain failed: %w", err)
	}
	matrixToPacket(pkt, processed)
	return nil
}

// HasProcessors checks if any processors are configured
func (pm *RowProcessors) HasProcessors() bool {
	return !pm.chain.IsEmpty()
}

// detectMaskPattern detects the appropriate mask pattern based on field name
func detectMaskPattern(fieldName string) processors.MaskPattern {
	lower := strings.ToLower(fieldName)

	switch {
	case strings.Contains(lower, "email"):
		return processors.MaskPartial
	case strings.Contains(lower, "phone") || strings.Contains(lower, "mobile"):
		return processors.MaskMiddle
	case strings.Contains(lower, "card") || strings.Contains(lower, "credit"):
		return processors.MaskFirst2Last2
	case strings.Contains(lower, "passport") || strings.Contains(lower, "ssn"):
		return processors.MaskStars
	default:
		return processors.MaskPartial
	}
}

// packetToMatrix splits rows the way the parser does, honouring escaped
// pipes. It used to be strings.Split(row, "|"): a value holding an escaped
// pipe shifted every later column by one, so --mask email masked the
// neighbouring field and left the address in clear.
func packetToMatrix(pkt *packet.DataPacket) [][]string {
	p := packet.NewParser()
	matrix := make([][]string, len(pkt.Data.Rows))
	for i, row := range pkt.Data.Rows {
		matrix[i] = p.GetRowValues(row)
	}
	return matrix
}

// matrixToPacket replaces the rows with the chain's output, whole, and
// re-escapes them. It used to overwrite only the first len(matrix) rows:
// when validate's on_error: filter dropped rows, the tail of the ORIGINAL
// rows stayed in the packet, so the filter passed the very rows it had just
// removed (all of them, when every row failed). RecordsInPart follows the
// new count.
func matrixToPacket(pkt *packet.DataPacket, matrix [][]string) {
	rows := make([]packet.Row, len(matrix))
	for i, values := range matrix {
		rows[i] = packet.Row{Value: packet.JoinRowEscaped(values)}
	}
	pkt.Data.Rows = rows
	pkt.Header.RecordsInPart = len(rows)
}
