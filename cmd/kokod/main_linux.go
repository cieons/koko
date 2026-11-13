package main

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/cieons/koko/internal/server"
	"github.com/cieons/koko/internal/server/filesystem"
	"github.com/cieons/koko/internal/server/installer"
	"github.com/cieons/koko/internal/server/kokod"
	"github.com/cieons/koko/internal/server/updatecheck"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	p := kokod.Program{Serve: serve, Machine: installer.System{}, Binary: os.Executable}
	status := p.Run(ctx, kokod.Invocation{
		Args:   os.Args[1:],
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Env:    os.Environ(),
	})
	stop()
	os.Exit(status)
}

// serve runs the koko server in this process on this machine's mounts,
// process uids and clock, telling it whether it runs in a container.
func serve(ctx context.Context, config server.Config) error {
	config.Filesystems = filesystem.System
	config.ProcessUIDs = unix.Getresuid
	config.InContainer = inContainer()
	config.UpdateClock = updatecheck.System
	config.MachineMemory = machineMemory
	return server.Run(ctx, config)
}

// machineMemory is the memory the koko server may use: the machine's, or
// its cgroup's limit, as a container's, when that is less, by cgroup v2's
// file or v1's; 0 when it cannot tell.
func machineMemory() uint64 {
	var info unix.Sysinfo_t
	if unix.Sysinfo(&info) != nil {
		return 0
	}
	total := info.Totalram * uint64(info.Unit)
	for _, file := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		b, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		if limit, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64); err == nil && limit < total {
			return limit
		}
	}
	return total
}

// inContainer tells whether this process runs in a container, from the file
// Docker (/.dockerenv) or Podman (/run/.containerenv) puts at its root.
func inContainer() bool {
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	return false
}
