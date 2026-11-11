package devicefiles

import (
	"errors"
	"io/fs"
)

// restrict would make path private to this OS user. Windows keeps that in
// ACLs, which the Windows CLI (P1) will set; until then a device cannot keep
// a join there.
func restrict(string, fs.FileMode) error {
	return errors.New("joining a koko server is not supported on Windows yet: koko cannot make its files private to this user there")
}

// writable would tell whether koko may rewrite the user's file at path.
// Until the Windows CLI (P1) reads ACLs, koko rewrites none there.
func writable(string, fs.FileInfo) bool {
	return false
}

// ownedByMe would tell whether this process's user owns the file fi
// describes. Windows keeps that in ACLs, which the Windows CLI (P1) will
// read; until then koko leaves such a file alone.
func ownedByMe(fs.FileInfo) bool {
	return false
}
