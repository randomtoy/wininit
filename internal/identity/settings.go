// Package identity defines bounded, non-secret desired account settings.
package identity

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

type User struct {
	Name                 string `yaml:"name" json:"name"`
	FullName             string `yaml:"full_name" json:"full_name"`
	Description          string `yaml:"description" json:"description"`
	PasswordFile         string `yaml:"password_file" json:"password_file"`
	PasswordNeverExpires bool   `yaml:"password_never_expires" json:"password_never_expires"`
}

type PasswordPolicy struct {
	MinimumLength          int `yaml:"minimum_length" json:"minimum_length"`
	MaximumAgeDays         int `yaml:"maximum_age_days" json:"maximum_age_days"`
	MinimumAgeDays         int `yaml:"minimum_age_days" json:"minimum_age_days"`
	HistoryLength          int `yaml:"history_length" json:"history_length"`
	LockoutThreshold       int `yaml:"lockout_threshold" json:"lockout_threshold"`
	LockoutDurationMinutes int `yaml:"lockout_duration_minutes" json:"lockout_duration_minutes"`
	LockoutWindowMinutes   int `yaml:"lockout_window_minutes" json:"lockout_window_minutes"`
}

type KeyUser struct {
	Name       string   `yaml:"name" json:"name"`
	PublicKeys []string `yaml:"public_keys" json:"public_keys"`
}

func DefaultPasswordPolicy() PasswordPolicy {
	return PasswordPolicy{MinimumLength: 14, MaximumAgeDays: 90, MinimumAgeDays: 1, HistoryLength: 24, LockoutThreshold: 10, LockoutDurationMinutes: 15, LockoutWindowMinutes: 15}
}

func ValidateName(name string) error {
	if len(name) < 1 || len(name) > 20 || !letter(name[0]) {
		return errors.New("account name must start with an ASCII letter and contain 1 through 20 characters")
	}
	for _, ch := range []byte(name) {
		if !letter(ch) && !(ch >= '0' && ch <= '9') && ch != '-' && ch != '_' {
			return errors.New("account name may contain only ASCII letters, digits, hyphens, and underscores")
		}
	}
	for _, reserved := range []string{"administrator", "guest", "defaultaccount", "wdagutilityaccount", "system", "localservice", "networkservice"} {
		if strings.EqualFold(name, reserved) {
			return errors.New("built-in account names are not supported")
		}
	}
	return nil
}

func letter(ch byte) bool { return ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' }

func LocalAbsolutePath(path string) bool {
	return filepath.IsAbs(path) && !strings.HasPrefix(path, `\\`) && !strings.HasPrefix(path, "//") && !strings.Contains(path, "://") && !strings.ContainsRune(path, 0)
}

func ValidateUsers(users []User) error {
	if len(users) < 1 || len(users) > 32 {
		return errors.New("local_users.users must contain 1 through 32 accounts")
	}
	seen := map[string]bool{}
	for index, user := range users {
		if err := ValidateName(user.Name); err != nil {
			return fmt.Errorf("local_users.users[%d]: %w", index, err)
		}
		name := strings.ToLower(user.Name)
		if seen[name] {
			return errors.New("local_users contains duplicate account names")
		}
		seen[name] = true
		if !LocalAbsolutePath(user.PasswordFile) {
			return errors.New("local_users password_file must be an absolute local path")
		}
		for _, text := range []string{user.FullName, user.Description} {
			if len(text) > 256 || strings.IndexFunc(text, unicode.IsControl) >= 0 {
				return errors.New("account full_name and description must contain at most 256 bytes without control characters")
			}
		}
	}
	return nil
}

func (p PasswordPolicy) Validate() error {
	if p.MinimumLength < 8 || p.MinimumLength > 14 || p.MaximumAgeDays < 1 || p.MaximumAgeDays > 365 || p.MinimumAgeDays < 0 || p.MinimumAgeDays > p.MaximumAgeDays || p.HistoryLength < 1 || p.HistoryLength > 24 {
		return errors.New("password_policy requires minimum_length 8..14, maximum_age_days 1..365, minimum_age_days 0..maximum_age_days, and history_length 1..24")
	}
	if p.LockoutThreshold < 1 || p.LockoutThreshold > 999 || p.LockoutWindowMinutes < 1 || p.LockoutWindowMinutes > 1440 || p.LockoutDurationMinutes < p.LockoutWindowMinutes || p.LockoutDurationMinutes > 1440 {
		return errors.New("password_policy requires lockout_threshold 1..999 and window/duration 1..1440 minutes with duration at least the window")
	}
	return nil
}
