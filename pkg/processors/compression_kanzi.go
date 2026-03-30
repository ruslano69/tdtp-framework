//go:build !nokanzi && !386

package processors

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"

	kio "github.com/flanglet/kanzi-go/v2/io"
)

// kanziPresets задаёт параметры transform+entropy для уровней kanzi.
var kanziPresets = map[int][2]string{
	6: {"TEXT+UTF+BWT+SRT+ZRLT", "FPAQ"},
	7: {"LZP+TEXT+UTF+BWT+LZP", "CM"},
}

const kanziDefaultLevel = 6

// CompressKanzi сжимает данные с помощью kanzi и кодирует результат в base64.
func CompressKanzi(input []byte, level int) ([]byte, error) {
	if len(input) == 0 {
		return nil, nil
	}

	preset, ok := kanziPresets[level]
	if !ok {
		preset = kanziPresets[kanziDefaultLevel]
	}
	transform, entropy := preset[0], preset[1]

	var buf bytes.Buffer
	w, err := kio.NewWriter(&nopWriteCloser{&buf}, transform, entropy, 1024*1024, 1, 0, int64(len(input)), false)
	if err != nil {
		return nil, fmt.Errorf("failed to create kanzi writer: %w", err)
	}
	if _, err = w.Write(input); err != nil {
		return nil, fmt.Errorf("kanzi compress write failed: %w", err)
	}
	if err = w.Close(); err != nil {
		return nil, fmt.Errorf("kanzi compress close failed: %w", err)
	}

	encoded := make([]byte, base64.StdEncoding.EncodedLen(buf.Len()))
	base64.StdEncoding.Encode(encoded, buf.Bytes())
	return encoded, nil
}

// DecompressKanzi декодирует данные из base64 и распаковывает с помощью kanzi.
func DecompressKanzi(input []byte) ([]byte, error) {
	if len(input) == 0 {
		return nil, nil
	}

	decoded := make([]byte, base64.StdEncoding.DecodedLen(len(input)))
	n, err := base64.StdEncoding.Decode(decoded, input)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	r, err := kio.NewReader(&nopReadCloser{bytes.NewReader(decoded[:n])}, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to create kanzi reader: %w", err)
	}
	defer func() { _ = r.Close() }()

	decompressed, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("kanzi decompress failed: %w", err)
	}
	return decompressed, nil
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

type nopReadCloser struct{ io.Reader }

func (nopReadCloser) Close() error { return nil }
