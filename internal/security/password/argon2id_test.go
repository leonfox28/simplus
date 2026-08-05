package password

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func testHasher(t *testing.T) *Hasher {
	t.Helper()
	hasher, err := NewHasher(Parameters{
		MemoryKiB:   8 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltBytes:   16,
		HashBytes:   16,
	})
	if err != nil {
		t.Fatal(err)
	}
	hasher.random = bytes.NewReader(bytes.Repeat([]byte{0x5a}, 16))
	return hasher
}

func TestHasherRoundTrip(t *testing.T) {
	hasher := testHasher(t)
	encoded, err := hasher.Hash("a correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded, "correct horse") {
		t.Fatal("encoded password hash contains plaintext")
	}
	matched, err := hasher.Verify(encoded, "a correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Fatal("correct password did not match")
	}
	matched, err = hasher.Verify(encoded, "wrong password")
	if err != nil {
		t.Fatal(err)
	}
	if matched {
		t.Fatal("wrong password matched")
	}
}

func TestHasherRejectsMalformedOrExcessiveHashes(t *testing.T) {
	hasher := testHasher(t)
	for _, encoded := range []string{
		"not-a-hash",
		"$argon2id$v1$m=8192,t=1,p=1$bad!$bad!",
		"$argon2id$v1$m=1048577,t=1,p=1$WlpaWlpaWlpaWlpaWlpaWg$WlpaWlpaWlpaWlpaWlpaWg",
	} {
		if _, err := hasher.Verify(encoded, "password"); !errors.Is(err, ErrInvalidHash) {
			t.Fatalf("Verify(%q) error = %v, want ErrInvalidHash", encoded, err)
		}
	}
}

func TestParametersFailClosed(t *testing.T) {
	if _, err := NewHasher(Parameters{}); err == nil {
		t.Fatal("NewHasher accepted zero parameters")
	}
	hasher := testHasher(t)
	hasher.random = bytes.NewReader(nil)
	if _, err := hasher.Hash("password"); err == nil {
		t.Fatal("Hash accepted a failed random source")
	}
}
