package commands

import "sync/atomic"

// quietOutput is the process-wide --quiet setting.
//
// Package-level state is normally the wrong answer, and every command that can
// reasonably carry the flag does carry it — SyncOptions.Quiet, MapOptions.Quiet,
// mapping.ExecOptions.Quiet. This exists for the prints buried several layers
// below any of those: the v1.5 packet UUID is emitted by DecryptPacketV15,
// reached through decryptV15PacketIfNeeded and parseAndDecryptBrokerMessage,
// neither of which has any other reason to know about output formatting. A
// per-message line there defeats --quiet in exactly the case it was added for
// (a drain consuming a dozen messages), and threading a bool through three
// signatures and five tests to silence one line buys nothing.
//
// Safe because it is what it looks like: one process, one CLI invocation, one
// verbosity, set once before any command runs. It is deliberately not a general
// output-mode mechanism — prefer the explicit option field wherever the call
// site already has one.
var quietOutput atomic.Bool

// SetQuietOutput records the process-wide --quiet setting. Called once from
// main before dispatch.
func SetQuietOutput(q bool) { quietOutput.Store(q) }

// QuietOutput reports whether --quiet is in effect.
func QuietOutput() bool { return quietOutput.Load() }
