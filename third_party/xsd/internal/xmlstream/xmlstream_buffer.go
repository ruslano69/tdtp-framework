package xmlstream

import (
	"errors"
	"io"
)

// byteStream tracks line and column positions in bytes, not runes; columns
// inside multibyte UTF-8 sequences report byte offsets.
type byteStream struct {
	r        io.Reader
	err      error
	lastPos  bytePosition
	off      int
	end      int
	line     int
	col      int
	maxBytes int64
	// admittedBytes excludes the single overflow probe so offsets refer only
	// to bytes exposed to tokenization.
	admittedBytes int64
	buf           [xmlInputBufferSize]byte
	unread        bool
	last          byte
	afterCR       bool
}

const (
	xmlInputBufferSize          = 64 * 1024
	maxConsecutiveEmptyXMLReads = 100
)

func (b *byteStream) reset(r io.Reader, maxBytes int64) {
	b.r = r
	b.err = nil
	b.lastPos = bytePosition{}
	b.off = 0
	b.end = 0
	b.line = 1
	b.col = 0
	b.maxBytes = maxBytes
	b.admittedBytes = 0
	b.unread = false
	b.last = 0
	b.afterCR = false
}

func (b *byteStream) detach() {
	b.r = nil
	b.err = nil
	b.off = 0
	b.end = 0
	b.unread = false
	b.last = 0
	b.afterCR = false
	b.maxBytes = 0
	b.admittedBytes = 0
}

// read admits at most maxBytes raw input bytes to the parser. It may consume
// one additional byte from the caller to prove that the limit was exceeded,
// but that byte is never exposed to tokenization. When the boundary-crossing
// read also fails, both causes are retained.
func (b *byteStream) read(p []byte) (int, error) {
	if b.maxBytes > 0 {
		remaining := b.maxBytes - b.admittedBytes
		if remaining < 0 {
			return 0, errXMLInputLimit
		}
		if int64(len(p)) > remaining {
			p = p[:remaining+1]
		}
	}
	n, err := b.readUnderlying(p)
	if n <= 0 {
		return n, err
	}
	if b.maxBytes <= 0 {
		b.admittedBytes += int64(n)
		return n, err
	}
	remaining := b.maxBytes - b.admittedBytes
	if int64(n) <= remaining {
		b.admittedBytes += int64(n)
		return n, err
	}
	admitted := int(remaining)
	b.admittedBytes += remaining
	limitErr := errXMLInputLimit
	if err != nil {
		limitErr = errors.Join(limitErr, err)
	}
	return admitted, limitErr
}

func (b *byteStream) readUnderlying(p []byte) (int, error) {
	for range maxConsecutiveEmptyXMLReads {
		n, err := b.r.Read(p)
		if n != 0 || err != nil {
			return n, err
		}
	}
	return 0, io.ErrNoProgress
}

// ensure returns the non-consuming input window after reading until at least n
// bytes are available, an error is known, or the fixed input buffer is full.
// An error returned with bytes is deferred when the window already satisfies n.
func (b *byteStream) ensure(n int) ([]byte, error) {
	for b.end-b.off < n && b.end < len(b.buf) {
		if b.err != nil {
			return b.buf[b.off:b.end], b.err
		}
		if b.r == nil {
			return b.buf[b.off:b.end], ErrXMLInputNilReader
		}
		read, err := b.read(b.buf[b.end:])
		if read > 0 {
			b.end += read
			b.err = err
			continue
		}
		if err != nil {
			b.err = err
			return b.buf[b.off:b.end], err
		}
		return b.buf[b.off:b.end], io.ErrNoProgress
	}
	return b.buf[b.off:b.end], nil
}

func (b *byteStream) discardUTF8BOM() {
	copy(b.buf[:], b.buf[utf8BOMLen:b.end])
	b.end -= utf8BOMLen
	b.off = 0
}

type bytePosition struct {
	line    int
	col     int
	afterCR bool
}

func (b *byteStream) readByte() (byte, error) {
	if b.unread {
		b.unread = false
		b.advance(b.last)
		return b.last, nil
	}
	if b.off == b.end {
		if err := b.fill(); err != nil {
			return 0, err
		}
	}
	c := b.buf[b.off]
	b.off++
	b.last = c
	b.lastPos = bytePosition{line: b.line, col: b.col, afterCR: b.afterCR}
	b.advance(c)
	return c, nil
}

func (b *byteStream) buffered() ([]byte, error) {
	if b.unread {
		return []byte{b.last}, nil
	}
	if err := b.fill(); err != nil {
		return nil, err
	}
	return b.buf[b.off:b.end], nil
}

func (b *byteStream) fill() error {
	if b.off != b.end {
		return nil
	}
	if b.err != nil {
		// Keep the stored error terminal. In particular, a limit probe leaves
		// admittedBytes at maxBytes, so retrying here would issue another read.
		return b.err
	}
	if b.r == nil {
		return ErrXMLInputNilReader
	}
	n, err := b.read(b.buf[:])
	if n > 0 {
		b.off = 0
		b.end = n
		b.err = err
		return nil
	}
	if err != nil {
		b.err = err
		return err
	}
	b.err = io.ErrNoProgress
	return b.err
}

// consumeBuffered advances past n bytes previously returned by buffered.
// Callers pass n > 0 after proving the bytes contain neither CR nor LF.
func (b *byteStream) consumeBuffered(n int) {
	if b.unread {
		b.unread = false
		b.advance(b.last)
		return
	}
	b.off += n
	b.col += n
	b.afterCR = false
}

func (b *byteStream) unreadByte() {
	if b.unread {
		panic("double unread")
	}
	b.unread = true
	b.line = b.lastPos.line
	b.col = b.lastPos.col
	b.afterCR = b.lastPos.afterCR
}

func (b *byteStream) advance(c byte) {
	// One range check keeps ordinary token bytes on the parser's hot path.
	if c-'\n' <= '\r'-'\n' {
		switch c {
		case '\r':
			b.line++
			b.col = 0
			b.afterCR = true
			return
		case '\n':
			if b.afterCR {
				b.afterCR = false
				return
			}
			b.line++
			b.col = 0
			return
		}
	}
	if b.afterCR {
		b.afterCR = false
	}
	b.col++
}

func (b *byteStream) pos() (line, column int) {
	return b.line, b.col
}

// offset reports the logical source position of the next byte. A byte that
// was unread belongs to the next token rather than the current one.
func (b *byteStream) offset() int {
	offset := b.admittedBytes - int64(b.end-b.off)
	if b.unread {
		offset--
	}
	if offset < 0 {
		return 0
	}
	return int(offset)
}

// Cache interns short parser strings per validation session. Each cache keeps
// at most maxByteStringCacheEntries strings no longer than
// maxByteStringCacheLen bytes.
type cache struct {
	state *cacheState
}

// The recent ring uses masking and must remain a power of two.
const (
	recentCacheEntries = 8
	recentCacheMask    = recentCacheEntries - 1
)

// Each map key and value share one owned spelling. The value lets a lookup
// using borrowed bytes return the owned string without allocating a copy.
type cacheState struct {
	interned map[string]string
	recent   [recentCacheEntries]string
	next     uint8
}

// Intern returns a cached string copy of b when b is small enough to cache.
func (c *cache) Intern(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	state := c.cacheState()
	if s, ok := state.recentString(b); ok {
		return s
	}
	if len(b) > maxByteStringCacheLen {
		return string(b)
	}
	if s, ok := state.interned[string(b)]; ok {
		state.remember(s)
		return s
	}
	s := string(b)
	if len(state.interned) >= maxByteStringCacheEntries {
		return s
	}
	state.interned[s] = s
	state.remember(s)
	return s
}

func (c *cache) cacheState() *cacheState {
	if c.state == nil {
		c.state = &cacheState{interned: make(map[string]string)}
	}
	return c.state
}

func (c *cacheState) recentString(b []byte) (string, bool) {
	for offset := range recentCacheEntries {
		index := (int(c.next) + recentCacheEntries - 1 - offset) & recentCacheMask
		s := c.recent[index]
		if stringBytesEqual(s, b) {
			return s, true
		}
	}
	return "", false
}

func (c *cacheState) remember(s string) {
	c.recent[c.next&recentCacheMask] = s
	c.next++
}
