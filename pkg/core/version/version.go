// Package version is the single source of truth for the tdtp-framework version.
//
// All components — the tdtpcli binary, the libtdtp shared library (J_GetVersion),
// and the Python bindings (__version__ via J_GetVersion at import time) — read the
// version from this constant. Do not hardcode the version anywhere else.
package version

// Version is the semantic version of the tdtp-framework.
const Version = "1.9.7"
