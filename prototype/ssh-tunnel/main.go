// PROTOTYPE, throwaway: never merged. See README.md.
//
// kokoproto answers one question: does the shape of ADR 0002, 0012 and 0013
// work with unchanged OpenSSH tooling? It is a koko server with a Gateway,
// `koko tunnel`, and the tools to set it up and measure it.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: kokoproto server|tunnel|join|revoke|delay|bench ...")
		os.Exit(2)
	}
	args := os.Args[2:]
	var code int
	switch os.Args[1] {
	case "server":
		code = runServer(args)
	case "tunnel":
		code = runTunnel(args)
	case "join":
		code = runJoin(args)
	case "revoke":
		code = runRevoke(args)
	case "delay":
		code = runDelay(args)
	case "bench":
		code = runBench(args)
	default:
		fmt.Fprintf(os.Stderr, "kokoproto: unknown command %q\n", os.Args[1])
		code = 2
	}
	os.Exit(code)
}
