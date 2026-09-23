// Package validate owns XML instance validation concerns.
package validate

import "github.com/jacoelho/xsd/xsderrors"

const (
	defaultMaxErrors                       = 100
	defaultMaxIdentityScopes               = 10_000
	defaultMaxIdentityEntries              = 100_000
	defaultMaxIdentityTupleBytes           = 4 << 10
	defaultMaxSchemaLocationNamespaces     = 256
	defaultMaxSchemaLocationNamespaceBytes = 64 << 10
	defaultMaxInstanceDepth                = 256
	defaultMaxInstanceAttributes           = 4_096
	defaultMaxInstanceTextBytes            = 4 << 20
	defaultMaxInstanceTokenBytes           = 4 << 20
	defaultMaxInstanceBytes                = 64 << 20
	// defaultMaxInstanceValueWork is the finite per-value evaluation ceiling.
	// Keep this literal in the validation owner so it does not depend on schema
	// construction limits or the builtin type count.
	defaultMaxInstanceValueWork uint64 = 4_194_502_132_335
)

// Options controls instance validation limits.
type Options struct {
	MaxErrors                       int
	MaxIdentityScopes               int
	MaxIdentityEntries              int
	MaxIdentityTupleBytes           int64
	MaxSchemaLocationNamespaces     int
	MaxSchemaLocationNamespaceBytes int64
	MaxInstanceDepth                int
	MaxInstanceAttributes           int
	MaxInstanceTextBytes            int64
	MaxInstanceTokenBytes           int64
	MaxInstanceBytes                int64
	MaxInstanceValueWork            uint64
}

// Limits is the normalized internal form of Options.
type Limits struct {
	Errors                       int
	IdentityScopes               int
	IdentityEntries              int
	IdentityTupleBytes           int64
	SchemaLocationNamespaces     int
	SchemaLocationNamespaceBytes int64
	InstanceDepth                int
	InstanceAttributes           int
	InstanceTextBytes            int64
	InstanceTokenBytes           int64
	InstanceBytes                int64
	InstanceValueWork            uint64
}

// NormalizeOptions validates options and returns runtime limits.
func NormalizeOptions(opts Options) (Limits, error) {
	if err := validateOptions(opts); err != nil {
		return Limits{}, err
	}
	return Limits{
		Errors:                       intLimitOrDefault(opts.MaxErrors, defaultMaxErrors),
		IdentityScopes:               intLimitOrDefault(opts.MaxIdentityScopes, defaultMaxIdentityScopes),
		IdentityEntries:              intLimitOrDefault(opts.MaxIdentityEntries, defaultMaxIdentityEntries),
		IdentityTupleBytes:           byteLimitOrDefault(opts.MaxIdentityTupleBytes, defaultMaxIdentityTupleBytes),
		SchemaLocationNamespaces:     intLimitOrDefault(opts.MaxSchemaLocationNamespaces, defaultMaxSchemaLocationNamespaces),
		SchemaLocationNamespaceBytes: byteLimitOrDefault(opts.MaxSchemaLocationNamespaceBytes, defaultMaxSchemaLocationNamespaceBytes),
		InstanceDepth:                intLimitOrDefault(opts.MaxInstanceDepth, defaultMaxInstanceDepth),
		InstanceAttributes:           intLimitOrDefault(opts.MaxInstanceAttributes, defaultMaxInstanceAttributes),
		InstanceTextBytes:            byteLimitOrDefault(opts.MaxInstanceTextBytes, defaultMaxInstanceTextBytes),
		InstanceTokenBytes:           byteLimitOrDefault(opts.MaxInstanceTokenBytes, defaultMaxInstanceTokenBytes),
		InstanceBytes:                byteLimitOrDefault(opts.MaxInstanceBytes, defaultMaxInstanceBytes),
		InstanceValueWork:            uintLimitOrDefault(opts.MaxInstanceValueWork, defaultMaxInstanceValueWork),
	}, nil
}

func validateOptions(opts Options) error {
	values := [...]struct {
		name  string
		value int64
	}{
		{"MaxErrors", int64(opts.MaxErrors)},
		{"MaxIdentityScopes", int64(opts.MaxIdentityScopes)},
		{"MaxIdentityEntries", int64(opts.MaxIdentityEntries)},
		{"MaxIdentityTupleBytes", opts.MaxIdentityTupleBytes},
		{"MaxSchemaLocationNamespaces", int64(opts.MaxSchemaLocationNamespaces)},
		{"MaxSchemaLocationNamespaceBytes", opts.MaxSchemaLocationNamespaceBytes},
		{"MaxInstanceDepth", int64(opts.MaxInstanceDepth)},
		{"MaxInstanceAttributes", int64(opts.MaxInstanceAttributes)},
		{"MaxInstanceTextBytes", opts.MaxInstanceTextBytes},
		{"MaxInstanceTokenBytes", opts.MaxInstanceTokenBytes},
		{"MaxInstanceBytes", opts.MaxInstanceBytes},
	}
	for _, option := range values {
		if option.value < 0 {
			return optionError(option.name + " cannot be negative")
		}
	}
	return nil
}

func intLimitOrDefault(value, def int) int {
	if value == 0 {
		return def
	}
	return value
}

func byteLimitOrDefault(value, def int64) int64 {
	if value == 0 {
		return def
	}
	return value
}

func uintLimitOrDefault(value, def uint64) uint64 {
	if value == 0 {
		return def
	}
	return value
}

func optionError(msg string) error {
	return xsderrors.Validation(xsderrors.CodeValidationOption, msg, nil)
}
