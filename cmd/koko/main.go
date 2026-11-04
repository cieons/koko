// Command koko is the koko CLI and server.
package main

import (
	"fmt"
	"os"
	"runtime/debug"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println(version())
		return
	}
	fmt.Fprintln(os.Stderr, "usage: koko version")
	os.Exit(2)
}

// version reports the module version the Go toolchain stamped from VCS at build time.
func version() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return "unknown"
}
