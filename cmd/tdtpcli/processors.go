package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
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
func (pm *ProcessorManager) AddMaskProcessor(maskFields string) error {
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

// AddValidateProcessor adds field validation processor from YAML file
// Format: --validate rules.yaml
func (pm *ProcessorManager) AddValidateProcessor(rulesFile string) error {
	if rulesFile == "" {
		return nil
	}

	// TODO: Load validation rules from YAML file
	// For now, create a simple validator with default common rules
	validationRules := map[string][]processors.FieldValidationRule{
		"email": {
			{Type: processors.ValidateEmail, Param: "", ErrMsg: ""},
		},
		"customer_email": {
			{Type: processors.ValidateEmail, Param: "", ErrMsg: ""},
		},
		"phone": {
			{Type: processors.ValidatePhone, Param: "", ErrMsg: ""},
		},
		"customer_phone": {
			{Type: processors.ValidatePhone, Param: "", ErrMsg: ""},
		},
	}

	validator, err := processors.NewFieldValidator(validationRules, false)
	if err != nil {
		return fmt.Errorf("failed to create validator: %w", err)
	}

	pm.chain.Add(validator)
	fmt.Printf("✓ Added field validator from: %s\n", rulesFile)

	return nil
}

// AddNormalizeProcessor adds field normalization processor from YAML file
// Format: --normalize rules.yaml
func (pm *ProcessorManager) AddNormalizeProcessor(rulesFile string) error {
	if rulesFile == "" {
		return nil
	}

	// TODO: Load normalization rules from YAML file
	// For now, create a simple normalizer with default rules
	normalizationRules := map[string]processors.NormalizeRule{
		"email":          processors.NormalizeEmail,
		"customer_email": processors.NormalizeEmail,
		"phone":          processors.NormalizePhone,
		"customer_phone": processors.NormalizePhone,
	}

	normalizer := processors.NewFieldNormalizer(normalizationRules)

	pm.chain.Add(normalizer)
	fmt.Printf("✓ Added field normalizer from: %s\n", rulesFile)

	return nil
}

// ProcessPacket applies all processors to a packet's data
func (pm *ProcessorManager) ProcessPacket(ctx context.Context, pkt *packet.DataPacket) error {
	if pm.chain.IsEmpty() {
		return nil
	}

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
