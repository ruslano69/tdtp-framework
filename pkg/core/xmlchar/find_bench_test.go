package xmlchar

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// A packet body's worth of typical row text: ASCII with some Cyrillic.
var benchBody = []byte(strings.Repeat("<R>12345|Иванов Пётр|ivanov.41@example.com|2026-08-21T14:38:11Z|1500.50</R>", 15000))

func BenchmarkFindBytes(b *testing.B) {
	b.SetBytes(int64(len(benchBody)))
	for i := 0; i < b.N; i++ {
		if _, bad := FindBytes(benchBody); bad {
			b.Fatal("clean body reported bad")
		}
	}
}

// For scale: the standard library's UTF-8 validation alone.
func BenchmarkUTF8Valid(b *testing.B) {
	b.SetBytes(int64(len(benchBody)))
	for i := 0; i < b.N; i++ {
		_ = utf8.Valid(benchBody)
	}
}

func BenchmarkCleanBytes(b *testing.B) {
	b.SetBytes(int64(len(benchBody)))
	for i := 0; i < b.N; i++ {
		if !CleanBytes(benchBody) {
			b.Fatal("clean body reported bad")
		}
	}
}
