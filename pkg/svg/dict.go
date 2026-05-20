package svg

import "github.com/ruslano69/tdtp-framework/pkg/core/packet"

// SVGDictionary is the well-known set of XML namespace URI shortcuts
// that pkg/svg uses to compress the `namespace` column.
//
// Tokens follow TDTP v1.4 dictionary semantics: whole-cell match only,
// `@` sigil, ASCII letters/digits/underscore after the sigil.
//
// Keep this list short and stable — adding entries is a one-way
// commitment (existing TDTP-SVG files in the wild that use a token
// would re-parse with the old meaning if we ever changed the URI).
// Adding NEW tokens is safe; renaming or removing is not.
var SVGDictionary = []packet.DictEntry{
	{Short: "@W3", Full: "http://www.w3.org/2000/svg"},
	{Short: "@xlink", Full: "http://www.w3.org/1999/xlink"},
	{Short: "@inks", Full: "http://www.inkscape.org/namespaces/inkscape"},
	{Short: "@sodi", Full: "http://sodipodi.sourceforge.net/DTD/sodipodi-0.0.dtd"},
	{Short: "@sketch", Full: "http://www.bohemiancoding.com/sketch/ns"},
}

// contractNamespace shortens a namespace URI to its dictionary token,
// or returns the input unchanged when no entry matches.
func contractNamespace(uri string) string {
	return packet.ContractDictionary(SVGDictionary, uri)
}

// expandNamespace reverses contractNamespace.
func expandNamespace(cell string) string {
	return packet.ExpandDictionary(SVGDictionary, cell)
}
