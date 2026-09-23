package xsd

import (
	"io"

	xsdSchema "github.com/jacoelho/xsd/internal/schema"
	"github.com/jacoelho/xsd/internal/validate"
)

// ValidateOptions controls instance validation.
type ValidateOptions struct {
	// MaxErrors limits reported recoverable validation errors. Once reached,
	// validation stops semantic assessment but continues reading XML until a
	// fatal error or the document end; a later fatal error takes precedence.
	// Zero uses the default.
	MaxErrors int
	// MaxIdentityScopes limits active identity-constraint scopes. Zero uses the default.
	MaxIdentityScopes int
	// MaxIdentityEntries independently limits stored ID, IDREF, key, unique,
	// and keyref entries, pending identity-selector matches, and pending
	// identity-field values. Zero means the default.
	MaxIdentityEntries int
	// MaxIdentityTupleBytes limits the byte length of one stored identity key. Zero uses the default.
	MaxIdentityTupleBytes int64
	// MaxSchemaLocationNamespaces limits distinct schema-location namespace
	// hints retained for one document. Zero uses the default of 256.
	MaxSchemaLocationNamespaces int
	// MaxSchemaLocationNamespaceBytes limits aggregate retained namespace-name
	// bytes. Zero uses the default of 64 KiB. MaxInstanceTokenBytes separately
	// bounds each complete hint attribute when configured.
	MaxSchemaLocationNamespaceBytes int64
	// MaxInstanceDepth limits nested XML elements. Zero uses the default.
	MaxInstanceDepth int
	// MaxInstanceAttributes limits attributes on one XML element. Zero uses the default.
	MaxInstanceAttributes int
	// MaxInstanceTextBytes limits retained character data bytes. Zero uses the default.
	MaxInstanceTextBytes int64
	// MaxInstanceTokenBytes limits parser-owned bytes for one XML token, including
	// retained payload and active construction scratch. Zero uses the default.
	MaxInstanceTokenBytes int64
	// MaxInstanceBytes limits aggregate raw XML bytes read. Zero uses the default.
	MaxInstanceBytes int64
	// MaxInstanceValueWork limits cumulative lexical and evaluation work for one
	// simple value. Each visited lexical byte and evaluation visit consumes one
	// work unit. Zero uses the finite default.
	MaxInstanceValueWork uint64
}

// Session validates XML instance documents against one Engine.
//
// Copies of a Session refer to the same reusable state. Concurrent use of one
// session, including through copies, fails with CodeValidationSession. Use
// Engine.Validate or separately constructed sessions for concurrent validation.
type Session struct {
	session *validate.Session
}

// Validate validates one XML instance document.
func (e *Engine) Validate(r io.Reader) error {
	return e.ValidateWithOptions(r, ValidateOptions{})
}

// ValidateWithOptions validates one XML instance document with options.
func (e *Engine) ValidateWithOptions(r io.Reader, opts ValidateOptions) error {
	var rt *xsdSchema.Schema
	if e != nil {
		rt = e.rt
	}
	return validate.Validate(rt, r, internalValidateOptions(opts))
}

// NewSession creates a reusable validation session. Reused sessions retain
// bounded scratch buffers and string caches; create a new session to release
// retained cache contents.
func (e *Engine) NewSession(opts ValidateOptions) (*Session, error) {
	var rt *xsdSchema.Schema
	if e != nil {
		rt = e.rt
	}
	inner, err := validate.NewSession(rt, internalValidateOptions(opts))
	if err != nil {
		return nil, err
	}
	return &Session{session: inner}, nil
}

// Validate validates one XML instance document. It clears document-local state
// before returning and may retain bounded scratch buffers and string caches for
// reuse.
func (s *Session) Validate(r io.Reader) error {
	if s == nil {
		return (*validate.Session)(nil).Validate(r)
	}
	return s.session.Validate(r)
}

func internalValidateOptions(opts ValidateOptions) validate.Options {
	return validate.Options{
		MaxErrors:                       opts.MaxErrors,
		MaxIdentityScopes:               opts.MaxIdentityScopes,
		MaxIdentityEntries:              opts.MaxIdentityEntries,
		MaxIdentityTupleBytes:           opts.MaxIdentityTupleBytes,
		MaxSchemaLocationNamespaces:     opts.MaxSchemaLocationNamespaces,
		MaxSchemaLocationNamespaceBytes: opts.MaxSchemaLocationNamespaceBytes,
		MaxInstanceDepth:                opts.MaxInstanceDepth,
		MaxInstanceAttributes:           opts.MaxInstanceAttributes,
		MaxInstanceTextBytes:            opts.MaxInstanceTextBytes,
		MaxInstanceTokenBytes:           opts.MaxInstanceTokenBytes,
		MaxInstanceBytes:                opts.MaxInstanceBytes,
		MaxInstanceValueWork:            opts.MaxInstanceValueWork,
	}
}
