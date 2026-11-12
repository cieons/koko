package harness

import (
	"fmt"
	"io/fs"
	"slices"
	"unsafe"

	"golang.org/x/sys/windows"
)

// notPrivate says how the file or directory at path, whose Lstat is fi,
// differs from one private to this OS user on Windows, or "" when it is
// private: this user owns it, and its access control list is protected, so
// that it inherits nothing, and holds one entry of its own for each of
// this user, SYSTEM (S-1-5-18) and Administrators (S-1-5-32-544), allowing
// full control, which a directory passes on to what is made in it. It reads
// the list by itself, sharing no code with what koko sets it with.
func notPrivate(path string, fi fs.FileInfo) string {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Sprintf("find this user: %v", err)
	}
	me := user.User.Sid.String()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Sprintf("read its security descriptor: %v", err)
	}
	if owner, _, err := sd.Owner(); err != nil || owner == nil || owner.String() != me {
		return fmt.Sprintf("its owner is %v (%v), want this user, %s", owner, err, me)
	}
	if control, _, err := sd.Control(); err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Sprintf("its access control list is not protected (control %#x, %v), so it inherits its directory's entries", control, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return fmt.Sprintf("it has no access control list (%v), which lets everyone in", err)
	}
	const fullControl = 0x1F01FF
	var wantFlags uint8
	if fi.IsDir() {
		wantFlags = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	}
	var got []string
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return fmt.Sprintf("read entry %d of its access control list: %v", i, err)
		}
		// Only an allow entry is laid out as ACCESS_ALLOWED_ACE is, so
		// its type is checked before its principal is read.
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fmt.Sprintf("entry %d of its access control list has type %d, want an allow entry (type 0)", i, ace.Header.AceType)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		if ace.Header.AceFlags != wantFlags || ace.Mask != fullControl {
			return fmt.Sprintf("its entry for %s has flags %#x and mask %#x, want an entry of its own (flags %#x) with full control (%#x)",
				sid, ace.Header.AceFlags, ace.Mask, wantFlags, fullControl)
		}
		got = append(got, sid)
	}
	want := []string{me, "S-1-5-18", "S-1-5-32-544"}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, slices.Compact(want)) {
		return fmt.Sprintf("its access control list names %v, want exactly this user, SYSTEM and Administrators, %v", got, want)
	}
	return ""
}

// access describes who may reach the file at path: its access control
// list, with whether it is protected, as Windows writes one down (SDDL).
func access(path string, _ fs.FileInfo) (string, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return "", err
	}
	return sd.String(), nil
}

// letOthersIn lets other users read the file or directory at path, so that
// it is no longer private to this OS user: its access control list gains
// an entry that allows Everyone to read it.
func letOthersIn(path string, _ fs.FileInfo) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read its security descriptor: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("read its access control list: %w", err)
	}
	everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		return err
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_READ,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(everyone),
		},
	}}, dacl)
	if err != nil {
		return fmt.Errorf("add an entry for Everyone: %w", err)
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}
