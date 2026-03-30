//go:build nokanzi || 386

package processors

import "fmt"

// CompressKanzi — заглушка для платформ без поддержки kanzi (386, nokanzi).
// kanzi-go использует untyped int константы > 2^31, переполняющие int на 32-bit.
func CompressKanzi(_ []byte, _ int) ([]byte, error) {
	return nil, fmt.Errorf("kanzi compression is not supported on this platform (use zstd)")
}

// DecompressKanzi — заглушка для платформ без поддержки kanzi.
func DecompressKanzi(_ []byte) ([]byte, error) {
	return nil, fmt.Errorf("kanzi decompression is not supported on this platform (use zstd)")
}
