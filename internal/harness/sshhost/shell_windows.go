package sshhost

import (
	"fmt"
	"os/exec"
	"sync"
)

// shell returns the Host Account's shell, which runs a session's command:
// on Windows the sh.exe of Git for Windows, found on PATH once. Windows has
// no /bin/sh, and a test's commands are written for one.
var shell = sync.OnceValues(func() (string, error) {
	sh, err := exec.LookPath("sh.exe")
	if err != nil {
		return "", fmt.Errorf("no sh.exe is on PATH, and the in-process Host runs a command through the sh.exe of Git for Windows: install Git for Windows and put its bin directory on PATH (%w)", err)
	}
	return sh, nil
})
