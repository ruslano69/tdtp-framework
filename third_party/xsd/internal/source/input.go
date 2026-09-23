package source

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"hash"
	"io"
	"math"

	"github.com/jacoelho/xsd/xsderrors"
)

const sourceInputBufferSize = 32 << 10

// Input is one bounded, forward-only view of a schema source. It retains
// reader state and digest state, never the source bytes. The owner must use
// the pointer linearly and must not copy or use it concurrently.
type Input struct {
	stream       io.ReadCloser
	digest       hash.Hash
	readErr      error
	name         string
	staticReader bytes.Reader

	static      [sha256.Size]byte
	max         int64
	staticBytes int64
	bytes       int64
	emptyReads  int

	hasStaticDigest bool
	limitExceeded   bool
	sourceDone      bool
	finished        bool
	finishStage     ReadStage
}

func newInput(name string, reader io.ReadCloser, maxBytes int64) *Input {
	return &Input{
		stream: reader,
		name:   name,
		max:    maxBytes,
		digest: sha256.New(),
	}
}

func newBytesInput(name string, data []byte, digest [sha256.Size]byte, maxBytes int64) *Input {
	input := &Input{
		name:            name,
		max:             maxBytes,
		static:          digest,
		staticBytes:     int64(len(data)),
		hasStaticDigest: true,
	}
	input.staticReader.Reset(data)
	return input
}

// Read implements io.Reader. It exposes at most the first maxBytes bytes plus
// one probe byte so callers can distinguish an exact boundary from overflow.
func (in *Input) Read(p []byte) (int, error) {
	if in == nil {
		return 0, io.ErrClosedPipe
	}
	if in.finished {
		return 0, io.EOF
	}
	if done, err := in.unavailableRead(); done {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if err := in.terminalError(); err != nil {
		return 0, err
	}
	if in.sourceDone {
		return 0, io.EOF
	}

	p = in.bound(p)
	n, err := in.readChunk(p)
	if n > 0 {
		in.record(p[:n])
	}
	return n, in.readResult(err)
}

// Finish drains unread input within the same bound, closes the source once,
// detaches it, and returns a stable result on every subsequent call.
func (in *Input) Finish() ReadResult {
	if in == nil {
		return ReadResult{
			Err:   errors.New("nil schema input"),
			Stage: ReadStageRead,
		}
	}
	if in.finished {
		return in.finalResult()
	}

	in.prepareFinish()
	in.drain()
	closeErr := in.closeSource()
	in.detach()
	in.finished = true
	result := in.result(closeErr)
	in.readErr = result.Err
	in.finishStage = result.Stage
	return result
}

func (in *Input) unavailableRead() (bool, error) {
	if in.hasStaticDigest || in.stream != nil {
		return false, nil
	}
	in.sourceDone = true
	in.readErr = invalidInputError()
	return true, in.readErr
}

func (in *Input) prepareFinish() {
	if in.hasStaticDigest {
		// Byte-backed sources are immutable and were checked against max at
		// open time. Their complete identity is already known, so a partial
		// parser read needs no drain or allocation.
		in.bytes = in.staticBytes
		in.sourceDone = true
		return
	}
	if in.stream == nil && in.digest == nil && in.readErr == nil {
		in.sourceDone = true
		in.readErr = invalidInputError()
	}
}

func (in *Input) drain() {
	if in.sourceDone || in.terminalError() != nil {
		return
	}
	buf := make([]byte, sourceInputBufferSize)
	for !in.sourceDone && in.terminalError() == nil {
		_, err := in.Read(buf)
		if err != nil {
			return
		}
	}
}

func (in *Input) closeSource() error {
	if in.stream == nil {
		return nil
	}
	return in.stream.Close()
}

func (in *Input) detach() {
	in.stream = nil
	if in.hasStaticDigest {
		in.staticReader.Reset(nil)
	}
}

func (in *Input) bound(p []byte) []byte {
	if in.max == math.MaxInt64 {
		return p
	}
	remaining := in.max - in.bytes
	if remaining < 0 {
		return p[:0]
	}
	// Permit one byte beyond the configured bound as the overflow probe.
	allowed := remaining + 1
	if allowed < int64(len(p)) {
		p = p[:allowed]
	}
	return p
}

func (in *Input) readChunk(p []byte) (int, error) {
	var n int
	var err error
	if in.hasStaticDigest {
		n, err = in.staticReader.Read(p)
	} else {
		n, err = in.stream.Read(p)
	}
	if n < 0 || n > len(p) {
		in.sourceDone = true
		return 0, io.ErrShortBuffer
	}
	if n != 0 || err != nil {
		in.emptyReads = 0
		return n, err
	}
	in.emptyReads++
	if in.emptyReads >= maxConsecutiveEmptySchemaReads {
		return 0, io.ErrNoProgress
	}
	return 0, nil
}

func (in *Input) readResult(err error) error {
	if err == io.EOF { //nolint:errorlint // Wrapped EOF is a source read failure; match io.ReadAll.
		in.sourceDone = true
		if in.overLimit() {
			in.setLimitExceeded()
			return in.terminalError()
		}
		// Returning EOF with data is legal, and lets callers stop without
		// another read while Finish still observes ordinary EOF.
		return io.EOF
	}
	if err != nil {
		in.sourceDone = true
		in.readErr = err
	}
	if in.overLimit() {
		in.setLimitExceeded()
		return in.terminalError()
	}
	if err == nil {
		return in.terminalError()
	}
	return err
}

func (in *Input) record(data []byte) {
	n := int64(len(data))
	if n > math.MaxInt64-in.bytes {
		in.bytes = math.MaxInt64
	} else {
		in.bytes += n
	}
	if !in.hasStaticDigest {
		_, _ = in.digest.Write(data)
	}
}

func (in *Input) overLimit() bool {
	return in.max != math.MaxInt64 && in.bytes > in.max
}

func (in *Input) setLimitExceeded() {
	if in.limitExceeded {
		return
	}
	in.limitExceeded = true
	limitErr := schemaSourceLimitError(in.name)
	if in.readErr == nil {
		in.readErr = limitErr
	} else {
		in.readErr = errors.Join(limitErr, in.readErr)
	}
}

func (in *Input) terminalError() error {
	return in.readErr
}

func (in *Input) result(closeErr error) ReadResult {
	result := ReadResult{
		Bytes:         in.bytes,
		Digest:        in.digestSum(),
		LimitExceeded: in.limitExceeded,
	}
	if err := in.terminalError(); err != nil {
		result.Err = err
		result.Stage = ReadStageRead
	}
	if closeErr != nil {
		if result.Err == nil {
			result.Err = closeErr
			result.Stage = ReadStageClose
		} else {
			result.Err = errors.Join(result.Err, closeErr)
		}
	}
	return result
}

func (in *Input) finalResult() ReadResult {
	return ReadResult{
		Err:           in.readErr,
		Bytes:         in.bytes,
		Digest:        in.digestSum(),
		Stage:         in.finishStage,
		LimitExceeded: in.limitExceeded,
	}
}

func (in *Input) digestSum() [sha256.Size]byte {
	if in.hasStaticDigest {
		return in.static
	}
	var result [sha256.Size]byte
	if in.digest == nil {
		return result
	}
	copy(result[:], in.digest.Sum(result[:0]))
	return result
}

func invalidInputError() error {
	return xsderrors.SchemaCompile(xsderrors.CodeSchemaRead, "schema input is invalid")
}
