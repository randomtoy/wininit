package identity

import (
	"bytes"
	"crypto/rsa"
	"errors"
	"sort"
	"strings"

	"golang.org/x/crypto/ssh"
)

const MaxAuthorizedKeysBytes = 256 << 10
const blockStart = "# BEGIN WININIT MANAGED KEYS"
const blockEnd = "# END WININIT MANAGED KEYS"

// NormalizeKeys rejects options, certificates, private material, and weak keys.
// Only canonical public keys, without comments, reach the platform adapter.
func NormalizeKeys(keys []string) ([]string, error) {
	if len(keys) > 64 {
		return nil, errors.New("authorized_keys permits at most 64 keys per account")
	}
	unique := map[string]bool{}
	for _, line := range keys {
		if len(line) > 16<<10 || strings.ContainsAny(line, "\r\n\x00") {
			return nil, errors.New("SSH public key must be one bounded line")
		}
		key, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil || len(options) != 0 || len(bytes.TrimSpace(rest)) != 0 {
			return nil, errors.New("invalid SSH public key or unsupported key options")
		}
		switch key.Type() {
		case ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
		case ssh.KeyAlgoRSA:
			cryptoKey, ok := key.(ssh.CryptoPublicKey)
			if !ok {
				return nil, errors.New("unsupported RSA public key")
			}
			rsaKey, ok := cryptoKey.CryptoPublicKey().(*rsa.PublicKey)
			if !ok || rsaKey.N.BitLen() < 3072 {
				return nil, errors.New("RSA SSH keys must contain at least 3072 bits")
			}
		default:
			return nil, errors.New("unsupported SSH public key algorithm")
		}
		unique[strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))] = true
	}
	result := make([]string, 0, len(unique))
	for key := range unique {
		result = append(result, key)
	}
	sort.Strings(result)
	return result, nil
}

func ValidateKeyUsers(users []KeyUser) error {
	if len(users) < 1 || len(users) > 32 {
		return errors.New("authorized_keys.users must contain 1 through 32 accounts")
	}
	seen := map[string]bool{}
	for _, user := range users {
		if err := ValidateName(user.Name); err != nil {
			return err
		}
		name := strings.ToLower(user.Name)
		if seen[name] {
			return errors.New("authorized_keys contains duplicate account names")
		}
		seen[name] = true
		if _, err := NormalizeKeys(user.PublicKeys); err != nil {
			return err
		}
	}
	return nil
}

// MergeKeys replaces only our marked block. Unmanaged bytes survive verbatim;
// an empty desired list revokes the managed block without deleting other keys.
func MergeKeys(existing []byte, canonical []string) ([]byte, error) {
	if len(existing) > MaxAuthorizedKeysBytes || bytes.IndexByte(existing, 0) >= 0 {
		return nil, errors.New("authorized_keys is too large or contains NUL")
	}
	start, end := -1, -1
	offset := 0
	for _, line := range bytes.SplitAfter(existing, []byte("\n")) {
		text := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
		switch text {
		case blockStart:
			if start != -1 || end != -1 {
				return nil, errors.New("authorized_keys has ambiguous managed markers")
			}
			start = offset
		case blockEnd:
			if start == -1 || end != -1 {
				return nil, errors.New("authorized_keys has ambiguous managed markers")
			}
			end = offset + len(line)
		}
		offset += len(line)
	}
	if (start == -1) != (end == -1) {
		return nil, errors.New("authorized_keys has an incomplete managed block")
	}
	var block []byte
	if len(canonical) > 0 {
		block = []byte(blockStart + "\n" + strings.Join(canonical, "\n") + "\n" + blockEnd + "\n")
	}
	var result []byte
	if start != -1 {
		result = append(result, existing[:start]...)
		result = append(result, block...)
		result = append(result, existing[end:]...)
	} else {
		result = append(result, existing...)
		if len(block) > 0 && len(result) > 0 && result[len(result)-1] != '\n' {
			result = append(result, '\n')
		}
		result = append(result, block...)
	}
	if len(result) > MaxAuthorizedKeysBytes {
		return nil, errors.New("merged authorized_keys exceeds 256 KiB")
	}
	return result, nil
}
