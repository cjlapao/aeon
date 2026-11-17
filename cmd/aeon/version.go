package main

import (
	"fmt"
	"io"
	"runtime"
)

// Set by the linker; see the Makefile's LDFLAGS.
var (
	Version   = "0.0.0-dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

// printVersion reports the build, and the Go toolchain that produced it.
//
// The Go version is included because `cgo` is on (B3) and the toolchain is part
// of what a binary IS — a bug report that says "aeon 0.3.1" and omits it costs
// a round trip on exactly the class of problem cgo introduces.
func printVersion(w io.Writer) {
	fmt.Fprintf(w, "aeon %s\n", Version)
	fmt.Fprintf(w, "  built   %s\n", BuildTime)
	fmt.Fprintf(w, "  commit  %s\n", GitCommit)
	fmt.Fprintf(w, "  go      %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
