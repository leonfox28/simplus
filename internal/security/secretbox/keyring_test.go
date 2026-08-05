package secretbox

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestKeyringPersistsAndAuthenticatesSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := first.Encrypt("tls-leaf", []byte("private key material"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("private key material")) {
		t.Fatal("ciphertext contains plaintext")
	}
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := second.Decrypt("tls-leaf", encoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != "private key material" {
		t.Fatalf("plaintext = %q", plaintext)
	}
	if _, err := second.Decrypt("wrong-label", encoded); err == nil {
		t.Fatal("wrong associated-data label was accepted")
	}
	encoded[len(encoded)-1] ^= 0xff
	if _, err := second.Decrypt("tls-leaf", encoded); err == nil {
		t.Fatal("tampered ciphertext was accepted")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 || info.Size() != 32 {
		t.Fatalf("master key mode/size = %04o/%d", info.Mode().Perm(), info.Size())
	}
}

func TestKeyringPseudonymIsStableAndDomainSeparated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	value, err := first.Pseudonym("sim-iccid-v1", []byte("8901000000000000001"))
	if err != nil {
		t.Fatal(err)
	}
	if len(value) != 64 || value == "8901000000000000001" {
		t.Fatalf("pseudonym = %q", value)
	}
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := second.Pseudonym("sim-iccid-v1", []byte("8901000000000000001"))
	if err != nil {
		t.Fatal(err)
	}
	otherLabel, err := second.Pseudonym("other-identity-v1", []byte("8901000000000000001"))
	if err != nil {
		t.Fatal(err)
	}
	if reopened != value || otherLabel == value {
		t.Fatalf("pseudonyms stable=%v separated=%v", reopened == value, otherLabel != value)
	}
}

func TestKeyringRejectsSymlinkAndUnsafeMode(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, bytes.Repeat([]byte{1}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if _, err := Open(link); err == nil {
		t.Fatal("symlink master key was accepted")
	}
	unsafe := filepath.Join(root, "unsafe")
	if err := os.WriteFile(unsafe, bytes.Repeat([]byte{1}, 32), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafe, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(unsafe); err == nil {
		t.Fatal("unsafe master key mode was accepted")
	}
}
