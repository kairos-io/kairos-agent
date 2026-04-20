package phonehome

import (
	"io"
	"log"
	"os"
)

// Logger is the package-level logger used by DefaultCommandHandler and
// Uninstall's helpers — anything below the Client where plumbing a
// *log.Logger parameter through every call site would be noise.
//
// Defaults match the Client's own internal logger (stderr, "[phonehome] "
// prefix, standard date/time), so a production agent shows the two sources
// interleaved consistently. Embedders that already run inside a logger
// pipeline (the kairos-agent CLI, tests, an embedding service) swap it out
// via SetLogger at startup.
var Logger = log.New(os.Stderr, "[phonehome] ", log.LstdFlags)

// SetLogger replaces the package logger. Passing nil silences output —
// useful in tests that don't want stdout/stderr noise under the specs.
func SetLogger(l *log.Logger) {
	if l == nil {
		Logger = log.New(io.Discard, "", 0)
		return
	}
	Logger = l
}
