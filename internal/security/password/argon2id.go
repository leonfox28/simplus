package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const encodedVersion = "v1"

var ErrInvalidHash = errors.New("invalid Argon2id password hash")

type Parameters struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltBytes   uint32
	HashBytes   uint32
}

func DefaultParameters() Parameters {
	return Parameters{
		MemoryKiB:   64 * 1024,
		Iterations:  3,
		Parallelism: 2,
		SaltBytes:   16,
		HashBytes:   32,
	}
}

type Hasher struct {
	parameters Parameters
	random     io.Reader
}

func NewHasher(parameters Parameters) (*Hasher, error) {
	if err := parameters.validate(); err != nil {
		return nil, err
	}
	return &Hasher{parameters: parameters, random: rand.Reader}, nil
}

func NewDefaultHasher() *Hasher {
	hasher, err := NewHasher(DefaultParameters())
	if err != nil {
		panic(err)
	}
	return hasher
}

func (hasher *Hasher) Hash(secret string) (string, error) {
	if hasher == nil || hasher.random == nil {
		return "", errors.New("password hasher is not configured")
	}
	salt := make([]byte, hasher.parameters.SaltBytes)
	if _, err := io.ReadFull(hasher.random, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	digest := argon2.IDKey(
		[]byte(secret),
		salt,
		hasher.parameters.Iterations,
		hasher.parameters.MemoryKiB,
		hasher.parameters.Parallelism,
		hasher.parameters.HashBytes,
	)
	return encode(hasher.parameters, salt, digest), nil
}

func (hasher *Hasher) Verify(encoded, secret string) (bool, error) {
	parameters, salt, expected, err := decode(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey(
		[]byte(secret),
		salt,
		parameters.Iterations,
		parameters.MemoryKiB,
		parameters.Parallelism,
		parameters.HashBytes,
	)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func encode(parameters Parameters, salt, digest []byte) string {
	base64Encoding := base64.RawStdEncoding
	return fmt.Sprintf(
		"$argon2id$%s$m=%d,t=%d,p=%d$%s$%s",
		encodedVersion,
		parameters.MemoryKiB,
		parameters.Iterations,
		parameters.Parallelism,
		base64Encoding.EncodeToString(salt),
		base64Encoding.EncodeToString(digest),
	)
}

func decode(encoded string) (Parameters, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != encodedVersion {
		return Parameters{}, nil, nil, ErrInvalidHash
	}
	var parameters Parameters
	values := strings.Split(parts[3], ",")
	if len(values) != 3 {
		return Parameters{}, nil, nil, ErrInvalidHash
	}
	memory, ok := parseParameter(values[0], "m")
	if !ok || memory > uint64(^uint32(0)) {
		return Parameters{}, nil, nil, ErrInvalidHash
	}
	iterations, ok := parseParameter(values[1], "t")
	if !ok || iterations > uint64(^uint32(0)) {
		return Parameters{}, nil, nil, ErrInvalidHash
	}
	parallelism, ok := parseParameter(values[2], "p")
	if !ok || parallelism > uint64(^uint8(0)) {
		return Parameters{}, nil, nil, ErrInvalidHash
	}

	base64Encoding := base64.RawStdEncoding.Strict()
	salt, err := base64Encoding.DecodeString(parts[4])
	if err != nil {
		return Parameters{}, nil, nil, ErrInvalidHash
	}
	digest, err := base64Encoding.DecodeString(parts[5])
	if err != nil {
		return Parameters{}, nil, nil, ErrInvalidHash
	}
	parameters = Parameters{
		MemoryKiB:   uint32(memory),
		Iterations:  uint32(iterations),
		Parallelism: uint8(parallelism),
		SaltBytes:   uint32(len(salt)),
		HashBytes:   uint32(len(digest)),
	}
	if err := parameters.validate(); err != nil {
		return Parameters{}, nil, nil, ErrInvalidHash
	}
	return parameters, salt, digest, nil
}

func parseParameter(value, name string) (uint64, bool) {
	prefix := name + "="
	if !strings.HasPrefix(value, prefix) || len(value) == len(prefix) {
		return 0, false
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, prefix), 10, 64)
	return parsed, err == nil
}

func (parameters Parameters) validate() error {
	switch {
	case parameters.MemoryKiB < 8*1024 || parameters.MemoryKiB > 1024*1024:
		return errors.New("Argon2id memory must be between 8192 and 1048576 KiB")
	case parameters.Iterations < 1 || parameters.Iterations > 16:
		return errors.New("Argon2id iterations must be between 1 and 16")
	case parameters.Parallelism < 1 || parameters.Parallelism > 16:
		return errors.New("Argon2id parallelism must be between 1 and 16")
	case parameters.SaltBytes < 16 || parameters.SaltBytes > 64:
		return errors.New("Argon2id salt length must be between 16 and 64 bytes")
	case parameters.HashBytes < 16 || parameters.HashBytes > 64:
		return errors.New("Argon2id hash length must be between 16 and 64 bytes")
	default:
		return nil
	}
}
