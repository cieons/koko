package devicefiles

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"unsafe"

	"golang.org/x/sys/windows"
)

// HomeVar names the environment variable that holds the home directory:
// on Windows the profile directory, where OpenSSH for Windows takes the home
// from too. HOME is never read there (ADR 0051).
const HomeVar = "USERPROFILE"

// fullControl is every right a file or a directory has (FILE_ALL_ACCESS),
// the one access mask of a private file's list.
const fullControl windows.ACCESS_MASK = windows.STANDARD_RIGHTS_ALL | 0x1FF

// principals returns the three principals a private file gives full control,
// and no other any access: this process's user, SYSTEM and Administrators,
// those OpenSSH for Windows itself accepts on a private key (ADR 0051). A
// user who is one of the other two is listed once.
func principals() (me *windows.SID, all []*windows.SID, err error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, nil, fmt.Errorf("find this process's user: %w", err)
	}
	me = user.User.Sid
	all = []*windows.SID{me}
	for _, known := range []windows.WELL_KNOWN_SID_TYPE{windows.WinLocalSystemSid, windows.WinBuiltinAdministratorsSid} {
		sid, err := windows.CreateWellKnownSid(known)
		if err != nil {
			return nil, nil, fmt.Errorf("find a well-known principal: %w", err)
		}
		if !slices.ContainsFunc(all, sid.Equals) {
			all = append(all, sid)
		}
	}
	return me, all, nil
}

// restrict makes path private to this OS user, where Linux and macOS set
// mode 0700 or 0600: its owner becomes this user, and its access control
// list a protected one, which inherits nothing, with full control for this
// user, SYSTEM and Administrators. A directory's entries pass to what is
// made in it. The mode is not read: Windows keeps none.
func restrict(path string, _ fs.FileMode) error {
	if err := setPrivate(path); err != nil {
		return fmt.Errorf("make %s private to this user: %w", path, err)
	}
	return nil
}

func setPrivate(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	me, all, err := principals()
	if err != nil {
		return err
	}
	acl, err := privateACL(all, fi.IsDir())
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		me, nil, acl, nil)
}

// MkdirPrivate makes the directory dir private to this OS user from the
// moment it exists, as restrict leaves koko's own directory: its owner is
// this user, and its access control list a protected one with full control
// for this user, SYSTEM and Administrators, passed on to what is made in
// it. No other process can find it open in between, as after os.Mkdir and
// restrict. It fails wrapping fs.ErrExist when something is at dir.
func MkdirPrivate(dir string) error {
	me, all, err := principals()
	if err != nil {
		return err
	}
	acl, err := privateACL(all, true)
	if err != nil {
		return err
	}
	sd, err := windows.NewSecurityDescriptor()
	if err == nil {
		err = sd.SetOwner(me, false)
	}
	if err == nil {
		err = sd.SetDACL(acl, true, false)
	}
	if err == nil {
		err = sd.SetControl(windows.SE_DACL_PROTECTED, windows.SE_DACL_PROTECTED)
	}
	if err != nil {
		return fmt.Errorf("make %s private to this user: %w", dir, err)
	}
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return &fs.PathError{Op: "mkdir", Path: dir, Err: err}
	}
	sa := windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(sa))
	if err := windows.CreateDirectory(p, &sa); err != nil {
		// ERROR_ALREADY_EXISTS matches fs.ErrExist.
		return &fs.PathError{Op: "mkdir", Path: dir, Err: err}
	}
	return nil
}

// closedToOthers says why no principal but this user, SYSTEM and
// Administrators may reach into the directory at path is untrue, or returns
// nil when it holds, where Linux and macOS ask for mode 0700: its access
// control list is protected, so it takes nothing from its parent, and each
// entry that allows any access, its own or one passed on to what the
// directory holds, names one of the three. An entry that denies access is
// no way in. A list koko cannot read through is not closed.
func closedToOthers(path string, _ fs.FileInfo) error {
	_, all, err := principals()
	if err != nil {
		return fmt.Errorf("cannot be checked: %w", err)
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("cannot be checked: %w", err)
	}
	control, _, err := sd.Control()
	if err != nil {
		return fmt.Errorf("cannot be checked: %w", err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return errors.New("takes access from the directory it is in: its access control list is not protected")
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("cannot be checked: %w", err)
	}
	if dacl == nil {
		return errors.New("has no access control list, which lets everyone in")
	}
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return fmt.Errorf("cannot be checked: entry %d of its access control list: %w", i, err)
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
		default:
			// Only these two are laid out as ACCESS_ALLOWED_ACE is.
			return fmt.Errorf("has an access control entry of type %d, which koko does not read", ace.Header.AceType)
		}
		if sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)); !slices.ContainsFunc(all, sid.Equals) {
			return fmt.Errorf("lets %s in", accountOf(sid))
		}
	}
	return nil
}

// accountOf names the principal sid as Windows shows it, such as
// BUILTIN\Users, with its SID.
func accountOf(sid *windows.SID) string {
	account, domain, _, err := sid.LookupAccount("")
	switch {
	case err != nil:
		return sid.String()
	case domain != "":
		account = domain + `\` + account
	}
	return fmt.Sprintf("%s (%s)", account, sid)
}

// privateACL is the access control list of a private file or directory:
// full control for each of all, passed on to what a directory holds.
func privateACL(all []*windows.SID, dir bool) (*windows.ACL, error) {
	var inherit uint32 = windows.NO_INHERITANCE
	if dir {
		inherit = windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT
	}
	entries := make([]windows.EXPLICIT_ACCESS, 0, len(all))
	for _, sid := range all {
		entries = append(entries, windows.EXPLICIT_ACCESS{
			AccessPermissions: fullControl,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       inherit,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_UNKNOWN,
				TrusteeValue: windows.TrusteeValueFromSID(sid),
			},
		})
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return nil, fmt.Errorf("build its access control list: %w", err)
	}
	return acl, nil
}

// isPrivate tells whether the file at path is as restrict leaves one, where
// Linux and macOS ask for mode 0600: this user owns it, and its list is
// protected and holds exactly one entry of its own for each of the three
// principals, allowing full control. A file koko cannot look at is not.
func isPrivate(path string, _ fs.FileInfo) bool {
	me, all, err := principals()
	if err != nil {
		return false
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.Equals(me) {
		return false
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return false
	}
	// A nil list would let everyone in.
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || int(dacl.AceCount) != len(all) {
		return false
	}
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return false
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags&windows.INHERITED_ACE != 0 || ace.Mask != fullControl {
			return false
		}
		// Each principal once: the count is theirs, so none is missing.
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		at := slices.IndexFunc(all, sid.Equals)
		if at < 0 {
			return false
		}
		all = slices.Delete(all, at, at+1)
	}
	return true
}

// keepAccess gives the new file tmp the access the user's file at path has,
// which tmp is about to replace, where Linux and macOS copy the mode: path's
// access control list, protected or inheriting as it is. koko rewrites the
// user's file without changing who may read or write it.
func keepAccess(tmp, path string, _ fs.FileInfo) error {
	info, dacl, err := accessOf(path)
	if err != nil {
		return fmt.Errorf("read the access control list of %s: %w", path, err)
	}
	if err := windows.SetNamedSecurityInfo(tmp, windows.SE_FILE_OBJECT, info, nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("give %s the access control list of %s: %w", tmp, path, err)
	}
	return nil
}

// accessOf reads the access control list of the file at path, and how to
// set it on another file as it is here: protected, or inheriting.
func accessOf(path string) (windows.SECURITY_INFORMATION, *windows.ACL, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return 0, nil, err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return 0, nil, err
	}
	control, _, err := sd.Control()
	if err != nil {
		return 0, nil, err
	}
	if control&windows.SE_DACL_PROTECTED != 0 {
		return windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION, dacl, nil
	}
	return windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION, dacl, nil
}

// ownedByMe tells whether this process's user owns the file at path: its
// owner is this user, or Administrators when this process holds that group
// enabled, since Windows makes Administrators the owner of what an elevated
// process creates. A file koko cannot look at is not.
func ownedByMe(path string, _ fs.FileInfo) bool {
	me, _, err := principals()
	if err != nil {
		return false
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return false
	}
	if owner.Equals(me) {
		return true
	}
	if !owner.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
		return false
	}
	// The zero Token asks about this thread's own, and counts a group only
	// when it is enabled there.
	elevated, err := windows.Token(0).IsMember(owner)
	return err == nil && elevated
}
