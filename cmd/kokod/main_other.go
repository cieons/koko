//go:build !linux

package main

import (
	"fmt"
	"os"
	"runtime"
)

// main refuses: the koko server runs on Linux only, so this build carries
// none of it.
func main() {
	fmt.Fprintf(os.Stderr, "kokod: kokod runs on Linux only; this kokod is built for %s/%s\n", runtime.GOOS, runtime.GOARCH)
	os.Exit(1)
}
