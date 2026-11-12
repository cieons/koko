package harness

import "os"

// osEnv are the variables a device's commands get on Windows beside its
// home and LOCALAPPDATA: the test's PATH, as Windows gives every process
// one, on which koko status looks ssh up.
func osEnv() []string {
	return []string{"PATH=" + os.Getenv("PATH")}
}
