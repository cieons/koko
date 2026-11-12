//go:build unix

package cli

import (
	"fmt"
	"os"
)

// kokoFile is the name of koko's file in the directory it is installed in.
const kokoFile = "koko"

// replaceable names the kokos koko update replaces, for its refusal of
// another.
const replaceable = "the koko install.sh put in ~/" + userKokoDir + ", or root's in " + rootKokoDir

// replaceFile puts the checked file in place of target, a file in the same
// filesystem: one rename, which leaves a koko that runs from target its
// file. Its error says that nothing was changed.
func replaceFile(checked, target string) error {
	if err := os.Rename(checked, target); err != nil {
		return fmt.Errorf("%w%s", err, nothingChanged)
	}
	return nil
}
