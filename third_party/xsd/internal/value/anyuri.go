package value

import "github.com/jacoelho/xsd/internal/uriref"

func anyURILength(normalized string) (uint32, error) {
	characters, err := uriref.Check(normalized)
	if err != nil {
		return 0, err
	}
	return checkedUint32(characters, "anyURI length exceeds uint32 limit")
}
