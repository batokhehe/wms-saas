package logger

import (
	"io"
	"os"
)

// stdout is isolated in its own file so tests can redirect log output by
// swapping this variable instead of reaching into os.Stdout globally.
var stdout = func() io.Writer { return os.Stdout }
