//go:build !nokanzi && !386

package processors

import (
	"encoding/base64"
	"fmt"

	kentropy "github.com/flanglet/kanzi-go/v2/entropy"
	ktransform "github.com/flanglet/kanzi-go/v2/transform"
)

// Structural, allocation-free inspection of a kanzi bitstream header, so a
// hostile packet is rejected before the decoder allocates memory sized by the
// header's own block-size field. A 100 KB packet declaring a 1 GiB block size
// otherwise drives the importer to ~2.7 GB of commit and crashes it on a
// memory-tight host (a decompression bomb that passes checksum and integrity,
// because the payload itself is genuine).
//
// The header layout mirrors kanzi-go v2.5.0 CompressedInputStream.readHeader
// for bitstream version 6, and the 24-bit header checksum is recomputed the
// same way, so a stream that kanzi-go would accept parses identically here.

const (
	kanziMagic       = 0x4B414E5A // "KANZ"
	kanziVersion     = 6          // kanzi-go 2.5.x writes v6 only
	kanziHeaderHash  = 0x1E35A7BD
	kanziTDTPBlock   = 1 << 20 // the only block size tdtp's writer ever emits
	kanziMaxOutput   = MaxDecompressedBytes
	kanziBlockSlack  = 2048 // kanzi's _EXTRA_BUFFER_SIZE, matched by the decoders
)

// kanziHeader is what the pre-decompression checks need; the rest of the
// header is parsed only to reach these fields and verify the checksum.
type kanziHeader struct {
	version    uint32
	ckSize     uint32
	entropy    uint32
	transform  uint64
	blockSize  int
	outputSize int64
	hasOutput  bool
	checksumOK bool
}

// msbBitReader reads big-endian, MSB-first, exactly like kanzi-go's
// DefaultInputBitStream — the order the header was written in.
type msbBitReader struct {
	data []byte
	pos  uint64 // absolute bit offset
}

func (r *msbBitReader) read(n uint) (uint64, error) {
	if n == 0 {
		return 0, nil
	}
	if r.pos+uint64(n) > uint64(len(r.data))*8 {
		return 0, fmt.Errorf("truncated at bit %d", r.pos)
	}
	var v uint64
	for i := uint(0); i < n; i++ {
		byteIdx := (r.pos + uint64(i)) >> 3
		bit := 7 - uint((r.pos+uint64(i))&7)
		v = (v << 1) | uint64((r.data[byteIdx]>>bit)&1)
	}
	r.pos += uint64(n)
	return v, nil
}

// parseKanziHeader parses the stream header and verifies its checksum. It does
// not decode any block and never allocates memory sized by the header.
func parseKanziHeader(raw []byte) (kanziHeader, *msbBitReader, error) {
	br := &msbBitReader{data: raw}
	var h kanziHeader

	magic, err := br.read(32)
	if err != nil {
		return h, nil, fmt.Errorf("stream too short for header: %w", err)
	}
	if magic != kanziMagic {
		return h, nil, fmt.Errorf("not a kanzi stream (magic %#x)", magic)
	}

	v, err := br.read(4)
	if err != nil {
		return h, nil, err
	}
	h.version = uint32(v)
	if h.version != kanziVersion {
		return h, nil, fmt.Errorf("bitstream version %d, only %d is accepted", h.version, kanziVersion)
	}

	ck, err := br.read(2)
	if err != nil {
		return h, nil, err
	}
	h.ckSize = uint32(ck)

	e, err := br.read(5)
	if err != nil {
		return h, nil, err
	}
	h.entropy = uint32(e)

	t, err := br.read(48)
	if err != nil {
		return h, nil, err
	}
	h.transform = t

	bs, err := br.read(28)
	if err != nil {
		return h, nil, err
	}
	h.blockSize = int(bs) << 4

	szMask, err := br.read(2)
	if err != nil {
		return h, nil, err
	}
	if szMask != 0 {
		out, err := br.read(16 * uint(szMask))
		if err != nil {
			return h, nil, err
		}
		h.outputSize = int64(out)
		h.hasOutput = true
	}

	if _, err := br.read(15); err != nil { // padding, v6
		return h, nil, err
	}

	stored, err := br.read(24)
	if err != nil {
		return h, nil, err
	}

	// uint32 arithmetic wraps mod 2^32, so it reproduces kanzi-go's header
	// checksum (which is computed on uint32) without explicit masking.
	hash := uint32(kanziHeaderHash) // a var, so the products below wrap at runtime
	c := hash * uint32(0x01030507*kanziVersion)
	c ^= hash * ^h.ckSize
	c ^= hash * ^h.entropy
	c ^= hash * uint32(^h.transform>>32)
	c ^= hash * uint32(^h.transform)
	c ^= hash * ^uint32(h.blockSize)
	if h.hasOutput {
		c ^= hash * uint32(^uint64(h.outputSize)>>32)
		c ^= hash * uint32(^uint64(h.outputSize))
	}
	c = (c >> 23) ^ (c >> 3)
	h.checksumOK = uint64(c&0xFFFFFF) == stored

	return h, br, nil
}

// walkBlockPrefixes verifies every block's declared length against the
// per-block bound (blockSize*1.5 + slack, the same bound kanzi-cpp and
// kanzi-rs apply) and against the bytes actually present, without decoding.
func walkBlockPrefixes(br *msbBitReader, blockSize int) error {
	maxBlockBits := uint64(blockSize+blockSize/2+kanziBlockSlack) * 8
	total := uint64(len(br.data)) * 8

	for n := 1; ; n++ {
		width, err := br.read(5)
		if err != nil {
			return fmt.Errorf("block %d: %w", n, err)
		}
		length, err := br.read(uint(width) + 3)
		if err != nil {
			return fmt.Errorf("block %d: %w", n, err)
		}
		if length == 0 {
			return nil // end-of-stream marker
		}
		if length > maxBlockBits {
			return fmt.Errorf("block %d declares %d bits, bound is %d", n, length, maxBlockBits)
		}
		if br.pos+length > total {
			return fmt.Errorf("block %d declares %d bits, only %d remain", n, length, total-br.pos)
		}
		br.pos += length
	}
}

// allowedKanziCodecs is the (entropy, transform) set of tdtp's own presets,
// derived from kanziPresets via kanzi-go's factories so it cannot drift from
// what CompressKanzi actually writes.
var allowedKanziCodecs = func() map[[2]uint64]bool {
	m := make(map[[2]uint64]bool)
	for _, p := range kanziPresets {
		tt, err1 := ktransform.GetType(p[0])
		et, err2 := kentropy.GetType(p[1])
		if err1 == nil && err2 == nil {
			m[[2]uint64{uint64(et), tt}] = true
		}
	}
	return m
}()

// VerifyKanziStreamStrict is the import-side forgery gate (Defense 1). It
// admits only what tdtp itself produces: version 6, one of the L6/L7 presets,
// a 1 MiB block size, a declared output within the decompression limit, a
// correct header checksum, and block prefixes that stay within bounds.
func VerifyKanziStreamStrict(b64 string) error {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("kanzi payload is not valid base64: %w", err)
	}

	h, br, err := parseKanziHeader(raw)
	if err != nil {
		return err
	}
	if !h.checksumOK {
		return fmt.Errorf("kanzi header checksum mismatch")
	}
	if h.blockSize != kanziTDTPBlock {
		return fmt.Errorf("declared block size %d, tdtp only writes %d", h.blockSize, kanziTDTPBlock)
	}
	if !allowedKanziCodecs[[2]uint64{uint64(h.entropy), h.transform}] {
		return fmt.Errorf("entropy/transform %d/%#x is not a tdtp preset", h.entropy, h.transform)
	}
	if !h.hasOutput || h.outputSize <= 0 || h.outputSize > kanziMaxOutput {
		return fmt.Errorf("declared output size %d is missing or exceeds %d", h.outputSize, kanziMaxOutput)
	}
	return walkBlockPrefixes(br, h.blockSize)
}

// guardKanziBlockSize is the decompressor-side safety bound (Defense 2). It is
// deliberately looser and codec-agnostic than the import gate: it only refuses
// a header whose block size would make the decoder allocate far more than any
// genuine tdtp stream needs, so a direct DecompressKanzi call (library API,
// broker path) cannot be turned into a memory bomb even without the import gate.
func guardKanziBlockSize(raw []byte) error {
	h, _, err := parseKanziHeader(raw)
	if err != nil {
		return err
	}
	if h.blockSize > kanziTDTPBlock {
		return fmt.Errorf("kanzi block size %d exceeds the %d limit", h.blockSize, kanziTDTPBlock)
	}
	return nil
}
