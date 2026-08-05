package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const (
	keyBytes      = 32
	formatVersion = byte(1)
	masterKeyMode = 0o600
)

type Keyring struct {
	aead         cipher.AEAD
	pseudonymKey []byte
}

func Open(path string) (*Keyring, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("secret key path must be absolute and clean")
	}
	key, err := loadOrCreateKey(path)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize secret key cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize secret key AEAD: %w", err)
	}
	return &Keyring{aead: aead, pseudonymKey: append([]byte(nil), key...)}, nil
}

// Pseudonym returns a stable, instance-scoped identifier without exposing the
// low-entropy source value. Labels provide domain separation between identity
// types and are part of the authenticated input.
func (keyring *Keyring) Pseudonym(label string, plaintext []byte) (string, error) {
	if keyring == nil || len(keyring.pseudonymKey) != keyBytes || label == "" || len(plaintext) == 0 {
		return "", errors.New("secret keyring, label, or pseudonym input is not configured")
	}
	mac := hmac.New(sha256.New, keyring.pseudonymKey)
	_, _ = mac.Write([]byte("simplus-pseudonym-v1\x00"))
	_, _ = mac.Write([]byte(label))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(plaintext)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func (keyring *Keyring) Encrypt(label string, plaintext []byte) ([]byte, error) {
	if keyring == nil || keyring.aead == nil || label == "" {
		return nil, errors.New("secret keyring or label is not configured")
	}
	nonce := make([]byte, keyring.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate secret nonce: %w", err)
	}
	sealed := keyring.aead.Seal(nil, nonce, plaintext, []byte(label))
	encoded := make([]byte, 1+len(nonce)+len(sealed))
	encoded[0] = formatVersion
	copy(encoded[1:], nonce)
	copy(encoded[1+len(nonce):], sealed)
	return encoded, nil
}

func (keyring *Keyring) Decrypt(label string, encoded []byte) ([]byte, error) {
	if keyring == nil || keyring.aead == nil || label == "" {
		return nil, errors.New("secret keyring or label is not configured")
	}
	nonceSize := keyring.aead.NonceSize()
	if len(encoded) < 1+nonceSize+keyring.aead.Overhead() || encoded[0] != formatVersion {
		return nil, errors.New("encrypted secret has an invalid format")
	}
	plaintext, err := keyring.aead.Open(nil, encoded[1:1+nonceSize], encoded[1+nonceSize:], []byte(label))
	if err != nil {
		return nil, errors.New("encrypted secret authentication failed")
	}
	return plaintext, nil
}

func loadOrCreateKey(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		key := make([]byte, keyBytes)
		if _, randomErr := io.ReadFull(rand.Reader, key); randomErr != nil {
			return nil, fmt.Errorf("generate secret master key: %w", randomErr)
		}
		created, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, masterKeyMode)
		if errors.Is(createErr, os.ErrExist) {
			return loadOrCreateKey(path)
		}
		if createErr != nil {
			return nil, fmt.Errorf("create secret master key: %w", createErr)
		}
		if _, writeErr := created.Write(key); writeErr != nil {
			_ = created.Close()
			_ = os.Remove(path)
			return nil, fmt.Errorf("write secret master key: %w", writeErr)
		}
		if syncErr := created.Sync(); syncErr != nil {
			_ = created.Close()
			_ = os.Remove(path)
			return nil, fmt.Errorf("sync secret master key: %w", syncErr)
		}
		if closeErr := created.Close(); closeErr != nil {
			_ = os.Remove(path)
			return nil, fmt.Errorf("close secret master key: %w", closeErr)
		}
		file, err = os.Open(path)
	}
	if err != nil {
		return nil, fmt.Errorf("open secret master key: %w", err)
	}
	defer file.Close()
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("lstat secret master key: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("secret master key must not be a symlink")
	}
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat secret master key: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != masterKeyMode {
		return nil, fmt.Errorf("secret master key must be a 0600 regular file, found mode %s", info.Mode())
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	pathStat, pathOK := pathInfo.Sys().(*syscall.Stat_t)
	if !ok || !pathOK || stat.Dev != pathStat.Dev || stat.Ino != pathStat.Ino {
		return nil, errors.New("secret master key path identity changed while opening")
	}
	if stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		return nil, errors.New("secret master key must be singly linked and owned by the service uid")
	}
	key, err := io.ReadAll(io.LimitReader(file, keyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read secret master key: %w", err)
	}
	if len(key) != keyBytes {
		return nil, fmt.Errorf("secret master key must contain exactly %d bytes", keyBytes)
	}
	return key, nil
}
