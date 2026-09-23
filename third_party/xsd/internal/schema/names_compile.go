package schema

import (
	"cmp"
	"maps"
	"slices"
)

// SortedQNames returns map keys ordered by expanded name text.
func SortedQNames[T any](m map[QName]T, names NameTable) []QName {
	return slices.SortedFunc(maps.Keys(m), func(a, b QName) int {
		aNS := names.Namespace(a.Namespace)
		bNS := names.Namespace(b.Namespace)
		if aNS != bNS {
			return cmp.Compare(aNS, bNS)
		}
		return cmp.Compare(names.Local(a.Local), names.Local(b.Local))
	})
}
