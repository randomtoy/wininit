package identity

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"golang.org/x/crypto/ssh"
	"strings"
	"testing"
)

func publicKey(t *testing.T) string {
	t.Helper()
	key, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public, err := ssh.NewPublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public)))
}
func TestKeyNormalizationAndManagedBlockPreservation(t *testing.T) {
	t.Parallel()
	key := publicKey(t)
	keys, err := NormalizeKeys([]string{key + " comment", key})
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Fatalf("normalize=%v %v", keys, err)
	}
	unmanaged := []byte("# operator-owned\r\nother-key user@host\r\n")
	merged, err := MergeKeys(unmanaged, keys)
	if err != nil || !bytes.HasPrefix(merged, unmanaged) {
		t.Fatalf("merge failed: %v", err)
	}
	repeated, err := MergeKeys(merged, keys)
	if err != nil || !bytes.Equal(repeated, merged) {
		t.Fatal("merge was not idempotent")
	}
	revoked, err := MergeKeys(merged, nil)
	if err != nil || !bytes.Equal(revoked, unmanaged) {
		t.Fatal("revocation changed unmanaged data")
	}
}
func TestKeyBoundaries(t *testing.T) {
	t.Parallel()
	key := publicKey(t)
	for _, value := range []string{"", "-----BEGIN PRIVATE KEY-----", "command=\"run\" " + key, key + "\n" + key, key + "\x00", "ssh-dss AAAA"} {
		if _, err := NormalizeKeys([]string{value}); err == nil {
			t.Fatal("invalid or option-bearing key accepted")
		}
	}
	for _, data := range []string{blockStart + "\n", blockEnd + "\n", blockStart + "\n" + blockStart + "\n" + blockEnd + "\n", blockStart + "\n" + blockEnd + "\n" + blockEnd + "\n", strings.Repeat("a", MaxAuthorizedKeysBytes+1), "key\x00"} {
		if _, err := MergeKeys([]byte(data), nil); err == nil {
			t.Fatal("ambiguous or oversized key file accepted")
		}
	}
}
func TestNamesPoliciesAndDuplicateUsers(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", "Administrator", "guest", "../user", "DOMAIN\\user", "name$", "user name", strings.Repeat("a", 21)} {
		if ValidateName(name) == nil {
			t.Fatalf("accepted name %q", name)
		}
	}
	if err := ValidateName("worker_01"); err != nil {
		t.Fatal(err)
	}
	if ValidateKeyUsers([]KeyUser{{Name: "worker"}, {Name: "WORKER"}}) == nil {
		t.Fatal("case-insensitive duplicate accepted")
	}
	valid := PasswordPolicy{14, 90, 1, 24, 10, 15, 15}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.LockoutDurationMinutes = 1
	if invalid.Validate() == nil {
		t.Fatal("invalid lockout policy accepted")
	}
}
func FuzzMergeKeys(f *testing.F) {
	f.Add([]byte("# unmanaged\n"))
	f.Add([]byte(blockStart + "\n" + blockEnd + "\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		merged, err := MergeKeys(data, nil)
		if err == nil {
			repeated, err := MergeKeys(merged, nil)
			if err != nil || !bytes.Equal(repeated, merged) {
				t.Fatal("revocation is not stable")
			}
		}
	})
}
