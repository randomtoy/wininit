//go:build windows

package identity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/internal/platform/securefile"
	"github.com/randomtoy/wininit/plugins/provisioner/localusers"
	"golang.org/x/sys/windows"
)

var (
	netapi                = windows.NewLazySystemDLL("netapi32.dll")
	netUserAdd            = netapi.NewProc("NetUserAdd")
	netUserSetInfo        = netapi.NewProc("NetUserSetInfo")
	netUserGetLocalGroups = netapi.NewProc("NetUserGetLocalGroups")
)

const (
	ufDisabled           = 0x2
	ufNormalAccount      = 0x200
	ufDontExpirePassword = 0x10000
)

// The layout matches USER_INFO_4, including native pointer alignment.
type userInfo4 struct {
	Name, Password                                                  *uint16
	PasswordAge, Priv                                               uint32
	HomeDir, Comment                                                *uint16
	Flags                                                           uint32
	ScriptPath                                                      *uint16
	AuthFlags                                                       uint32
	FullName, UserComment, Parameters, Workstations                 *uint16
	LastLogon, LastLogoff, AccountExpires, MaxStorage, UnitsPerWeek uint32
	LogonHours                                                      *byte
	BadPasswordCount, NumLogons                                     uint32
	LogonServer                                                     *uint16
	CountryCode, CodePage                                           uint32
	SID                                                             *windows.SID
	PrimaryGroupID                                                  uint32
	Profile, HomeDirDrive                                           *uint16
	PasswordExpired                                                 uint32
}
type userInfo1 struct {
	Name, Password    *uint16
	PasswordAge, Priv uint32
	HomeDir, Comment  *uint16
	Flags             uint32
	ScriptPath        *uint16
}

func localOnly(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var joinedName *uint16
	var status uint32
	if err := windows.NetGetJoinInformation(nil, &joinedName, &status); err != nil {
		return errors.New("query local machine membership failed")
	}
	if joinedName != nil {
		defer windows.NetApiBufferFree((*byte)(unsafe.Pointer(joinedName)))
	}
	if status == windows.NetSetupDomainName {
		return errors.New("identity provisioning refuses domain-joined computers and domain controllers")
	}
	return ctx.Err()
}

func readUser(ctx context.Context, name string) (*userInfo4, func(), error) {
	if err := localOnly(ctx); err != nil {
		return nil, nil, err
	}
	if err := identity.ValidateName(name); err != nil {
		return nil, nil, err
	}
	encoded, _ := windows.UTF16PtrFromString(name)
	var buffer *byte
	if err := windows.NetUserGetInfo(nil, encoded, 4, &buffer); err != nil {
		if errors.Is(err, windows.Errno(2221)) {
			return nil, func() {}, nil
		} // NERR_UserNotFound
		return nil, nil, errors.New("read local account failed")
	}
	cleanup := func() { windows.NetApiBufferFree(buffer) }
	info := (*userInfo4)(unsafe.Pointer(buffer))
	if info.SID == nil || !info.SID.IsValid() || info.Flags&ufNormalAccount == 0 || info.Flags&ufDisabled != 0 {
		cleanup()
		return nil, nil, errors.New("only enabled standard local accounts are supported")
	}
	sid := info.SID.String()
	for _, rid := range []string{"-500", "-501", "-503", "-504"} {
		if strings.HasSuffix(sid, rid) {
			cleanup()
			return nil, nil, errors.New("built-in account mutation is refused")
		}
	}
	admin, err := administrator(name)
	if err != nil || admin {
		cleanup()
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, errors.New("administrator accounts are not supported; their OpenSSH key file is shared")
	}
	return info, cleanup, nil
}

func administrator(name string) (bool, error) {
	adminSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return false, err
	}
	adminName, _, _, err := adminSID.LookupAccount("")
	if err != nil {
		return false, errors.New("resolve Administrators alias failed")
	}
	encoded, _ := windows.UTF16PtrFromString(name)
	var buffer *byte
	var count, total uint32
	code, _, _ := netUserGetLocalGroups.Call(0, uintptr(unsafe.Pointer(encoded)), 0, 1, uintptr(unsafe.Pointer(&buffer)), ^uintptr(0)&0xffffffff, uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&total)))
	if code != 0 {
		return false, fmt.Errorf("query local account groups failed (code %d)", code)
	}
	if buffer != nil {
		defer windows.NetApiBufferFree(buffer)
	}
	for _, group := range unsafe.Slice((**uint16)(unsafe.Pointer(buffer)), int(count)) {
		if strings.EqualFold(windows.UTF16PtrToString(group), adminName) {
			return true, nil
		}
	}
	return false, nil
}

func (*Adapter) User(ctx context.Context, name string) (localusers.UserState, bool, error) {
	info, cleanup, err := readUser(ctx, name)
	if err != nil {
		return localusers.UserState{}, false, err
	}
	defer cleanup()
	if info == nil {
		return localusers.UserState{}, false, nil
	}
	return localusers.UserState{
		FullName: windows.UTF16PtrToString(info.FullName), Description: windows.UTF16PtrToString(info.Comment),
		PasswordNeverExpires: info.Flags&ufDontExpirePassword != 0,
	}, true, nil
}

func (*Adapter) ReadPassword(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := securefile.TrustedFile(path, true)
	if err != nil {
		return nil, fmt.Errorf("password file security check failed: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 1025))
	if err != nil {
		clear(data)
		return nil, errors.New("read password file failed")
	}
	if err := ctx.Err(); err != nil {
		clear(data)
		return nil, err
	}
	// Permit one editor-added terminal line ending; preserve password spaces.
	password := data
	if bytes.HasSuffix(password, []byte("\n")) {
		password = bytes.TrimSuffix(password, []byte("\n"))
		password = bytes.TrimSuffix(password, []byte("\r"))
	}
	if len(data) > 1024 || len(password) == 0 || !utf8.Valid(password) || bytes.ContainsAny(password, "\r\n\x00") {
		clear(data)
		return nil, errors.New("password file must contain one nonempty UTF-8 password line no larger than 1024 bytes")
	}
	result := bytes.Clone(password)
	clear(data)
	return result, nil
}

func (a *Adapter) CreateUser(ctx context.Context, desired identity.User, password []byte) error {
	if err := localOnly(ctx); err != nil {
		return err
	}
	if err := identity.ValidateName(desired.Name); err != nil {
		return err
	}
	if len(password) == 0 || !utf8.Valid(password) || bytes.ContainsAny(password, "\r\n\x00") {
		return errors.New("invalid initial password")
	}
	// Avoid an immutable password string. All explicitly allocated byte/rune/
	// UTF-16 buffers are cleared after the API call; Windows still owns its copies.
	runes := make([]rune, 0, len(password))
	for len(password) > 0 {
		ch, size := utf8.DecodeRune(password)
		runes = append(runes, ch)
		password = password[size:]
	}
	encoded := utf16.Encode(runes)
	clear(runes)
	encodedPassword := make([]uint16, len(encoded)+1)
	copy(encodedPassword, encoded)
	clear(encoded)
	defer clear(encodedPassword)
	if len(encodedPassword) > 257 {
		return errors.New("initial password exceeds 256 UTF-16 code units")
	}
	name, _ := windows.UTF16PtrFromString(desired.Name)
	comment, _ := windows.UTF16PtrFromString(desired.Description)
	flags := uint32(ufNormalAccount | 1)
	if desired.PasswordNeverExpires {
		flags |= ufDontExpirePassword
	}
	info := userInfo1{Name: name, Password: &encodedPassword[0], Priv: 1, Comment: comment, Flags: flags}
	var parameter uint32
	code, _, _ := netUserAdd.Call(0, 1, uintptr(unsafe.Pointer(&info)), uintptr(unsafe.Pointer(&parameter)))
	runtime.KeepAlive(info)
	if code != 0 {
		return fmt.Errorf("create standard local account failed (code %d)", code)
	}
	// A failure here leaves a visible failed checkpoint. The next run repairs
	// attributes without resetting the account's now-existing password.
	return a.UpdateUser(ctx, desired)
}

func (*Adapter) UpdateUser(ctx context.Context, desired identity.User) error {
	info, cleanup, err := readUser(ctx, desired.Name)
	if err != nil {
		return err
	}
	defer cleanup()
	if info == nil {
		return errors.New("local account disappeared before reconciliation")
	}
	name, _ := windows.UTF16PtrFromString(desired.Name)
	fullName, _ := windows.UTF16PtrFromString(desired.FullName)
	comment, _ := windows.UTF16PtrFromString(desired.Description)
	flags := info.Flags &^ ufDontExpirePassword
	if desired.PasswordNeverExpires {
		flags |= ufDontExpirePassword
	}
	// Narrow information levels preserve account expiry, group memberships,
	// password expiry/age, login restrictions, and the existing password.
	for _, field := range []struct {
		level uintptr
		value unsafe.Pointer
	}{
		{1007, unsafe.Pointer(&comment)}, {1011, unsafe.Pointer(&fullName)}, {1008, unsafe.Pointer(&flags)},
	} {
		if err := ctx.Err(); err != nil {
			return err
		}
		var parameter uint32
		code, _, _ := netUserSetInfo.Call(0, uintptr(unsafe.Pointer(name)), field.level, uintptr(field.value), uintptr(unsafe.Pointer(&parameter)))
		if code != 0 {
			return fmt.Errorf("update local account attributes failed (code %d)", code)
		}
	}
	return nil
}
