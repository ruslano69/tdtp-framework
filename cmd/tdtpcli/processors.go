package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
	"gopkg.in/yaml.v3"
)

// ProcessorManager manages data processors for CLI
type ProcessorManager struct {
	chain *processors.Chain
}

// NewProcessorManager creates a new processor manager
func NewProcessorManager() *ProcessorManager {
	return &ProcessorManager{
		chain: processors.NewChain(),
	}
}

// AddMaskProcessor adds field masking processor from CLI flag
// Format: --mask email,phone,card
func (pm *ProcessorManager) AddMaskProcessor(maskFields string) error { //nolint:unparam // error return kept for API consistency
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
		fmt.Printf("✓ Added field masker: %d field(s)\n", len(fieldsToMask))
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
// If the file does not contain a "rules" section, validation is skipped silently.
func (pm *ProcessorManager) AddValidateProcessor(rulesFile string) error {
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
		// No validation section in file — skip silently
		return nil
	}

	validator, err := processors.NewFieldValidatorFromConfig(params)
	if err != nil {
		return fmt.Errorf("failed to create validator from %q: %w", rulesFile, err)
	}

	pm.chain.Add(validator)
	fmt.Printf("✓ Added field validator from: %s\n", rulesFile)

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
// If the file does not contain a "fields" section, normalization is skipped silently.
func (pm *ProcessorManager) AddNormalizeProcessor(rulesFile string) error {
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
		// No normalization section in file — skip silently
		return nil
	}

	normalizer, err := processors.NewFieldNormalizerFromConfig(params)
	if err != nil {
		return fmt.Errorf("failed to create normalizer from %q: %w", rulesFile, err)
	}

	pm.chain.Add(normalizer)
	fmt.Printf("✓ Added field normalizer from: %s\n", rulesFile)

	return nil
}

// Name implements processors.PacketProcessor.
func (pm *ProcessorManager) Name() string { return "row-chain" }

// ProcessPacket applies all processors to a packet's data
func (pm *ProcessorManager) ProcessPacket(ctx context.Context, pkt *packet.DataPacket) error {
	if pm.chain.IsEmpty() {
		return nil
	}

	// Материализуем rawRows (GenerateReference fast-path) — иначе Data.Rows пуст
	// и mask/normalize/validate молча пропускаются.
	pkt.MaterializeRows()

	// Convert packet data to [][]string format
	data := convertPacketToMatrix(pkt)

	// Apply processors
	processed, err := pm.chain.Process(ctx, data, pkt.Schema)
	if err != nil {
		return fmt.Errorf("processor chain failed: %w", err)
	}

	// Update packet with processed data
	updatePacketFromMatrix(pkt, processed)

	return nil
}

// HasProcessors checks if any processors are configured
func (pm *ProcessorManager) HasProcessors() bool {
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

// convertPacketToMatrix converts packet data to [][]string format for processing
func convertPacketToMatrix(pkt *packet.DataPacket) [][]string {
	rows := pkt.Data.Rows
	matrix := make([][]string, len(rows))

	for i, row := range rows {
		// Split row by delimiter (TDTP uses | as delimiter)
		values := strings.Split(row.Value, "|")
		matrix[i] = values
	}

	return matrix
}

// updatePacketFromMatrix updates packet data from processed matrix
func updatePacketFromMatrix(pkt *packet.DataPacket, matrix [][]string) {
	for i, row := range matrix {
		if i < len(pkt.Data.Rows) {
			// Join values back with delimiter
			pkt.Data.Rows[i].Value = strings.Join(row, "|")
		}
	}
}
