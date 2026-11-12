package devicefiles

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// knownHostsArg returns the UserKnownHostsFile argument of koko's ssh_config:
// on Windows koko's known_hosts under ~, whatever the home directory. Both
// ssh clients expand ~ to the home whose .ssh/config they read, OpenSSH for
// Windows to the user's profile directory, which is where koko writes, and
// Git for Windows' to HOME; and a path with no drive letter raises no
// question of quoting or of how Git's client reads "C:" (ADR 0051).
func knownHostsArg(string) (string, error) {
	return "~/" + kokoDir + "/" + knownHostsFile, nil
}

// sshWriteRights are the rights OpenSSH for Windows lets no principal but
// the trusted ones hold on a user's ssh configuration, SSH_SECURE_WRITE_MASK
// in its w32-sshfileperm.c; releases up to 9.5 check only the first four.
const sshWriteRights = windows.FILE_WRITE_DATA | windows.FILE_WRITE_ATTRIBUTES | windows.FILE_WRITE_EA |
	windows.FILE_APPEND_DATA | windows.WRITE_DAC | windows.WRITE_OWNER | windows.DELETE

// trustedInstaller is NT SERVICE\TrustedInstaller, which OpenSSH for
// Windows trusts beside this user, SYSTEM and Administrators.
const trustedInstaller = "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"

// sshRefuses says why OpenSSH for Windows would refuse the user's ssh
// configuration at path, as its check_secure_file_permission does, or
// returns nil: the file's owner is not this user, SYSTEM, Administrators or
// TrustedInstaller, or its access control list is none, which lets
// everyone in, or an entry of it, its own or inherited, allows another
// principal to write it. OpenSSH then fails every ssh with "Bad owner or
// permissions". koko only says so: it changes no file it does not own
// (ADR 0051). A list koko cannot read is reported as such, never as one
// OpenSSH reads.
func sshRefuses(path string) error {
	unknown := func(err error) error {
		return fmt.Errorf("koko cannot read who may change ~/%s (%w), so it cannot tell whether OpenSSH for Windows reads it", userConfig, err)
	}
	me, _, err := principals()
	if err != nil {
		return unknown(err)
	}
	ti, err := windows.StringToSid(trustedInstaller)
	if err != nil {
		return unknown(err)
	}
	trusted := func(sid *windows.SID) bool {
		return sid.Equals(me) || sid.Equals(ti) ||
			sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) || sid.IsWellKnown(windows.WinLocalSystemSid)
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return unknown(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return unknown(err)
	}
	if owner == nil || !trusted(owner) {
		who := "no principal"
		if owner != nil {
			who = accountOf(owner)
		}
		return fmt.Errorf("OpenSSH for Windows refuses ~/%s, which %s owns, and then reaches no host at all: run icacls \"%s\" /setowner *%s to make the file yours",
			userConfig, who, path, me)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return unknown(err)
	}
	if dacl == nil {
		return fmt.Errorf("OpenSSH for Windows refuses ~/%s, which has no access control list, so that everyone may write it, and then reaches no host at all: run icacls \"%s\" /reset to give it the one its directory passes on",
			userConfig, path)
	}
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return unknown(err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Mask&sshWriteRights == 0 {
			continue
		}
		if sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)); !trusted(sid) {
			return fmt.Errorf("OpenSSH for Windows refuses ~/%s, which %s may write, and then reaches no host at all: run icacls \"%s\" /inheritance:d /remove:g *%s to take that access away",
				userConfig, accountOf(sid), path, sid)
		}
	}
	return nil
}
