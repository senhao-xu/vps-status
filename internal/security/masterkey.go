package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const masterKeySize = 32

var secretAAD = []byte("vps-status/secret/v1")

// MasterKey holds the deployment-local AES-256 key.
type MasterKey struct {
	value [masterKeySize]byte
}

// LoadOrCreateMasterKey loads a strictly private key file or atomically creates
// one with mode 0600. Existing group/world permissions are rejected.
func LoadOrCreateMasterKey(path string) (*MasterKey, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create master-key directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return createMasterKey(path)
	}
	if err != nil {
		return nil, fmt.Errorf("open master key: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat master key: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("master key must be a regular file accessible only by its owner")
	}
	var key MasterKey
	if _, err := io.ReadFull(file, key.value[:]); err != nil {
		return nil, errors.New("master key has invalid length")
	}
	var extra [1]byte
	if count, readErr := file.Read(extra[:]); count != 0 || readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, errors.New("master key has invalid length")
	}
	return &key, nil
}

func createMasterKey(path string) (*MasterKey, error) {
	var key MasterKey
	if _, err := rand.Read(key.value[:]); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return LoadOrCreateMasterKey(path)
	}
	if err != nil {
		return nil, fmt.Errorf("create master key: %w", err)
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(key.value[:]); err != nil {
		return nil, fmt.Errorf("write master key: %w", err)
	}
	if err := file.Sync(); err != nil {
		return nil, fmt.Errorf("sync master key: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close master key: %w", err)
	}
	ok = true
	return &key, nil
}

// Encrypt encrypts a secret with AES-GCM and a unique random nonce.
func (key *MasterKey) Encrypt(plaintext []byte) ([]byte, error) {
	gcm, err := key.gcm()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate encryption nonce: %w", err)
	}
	result := make([]byte, 1, 1+len(nonce)+len(plaintext)+gcm.Overhead())
	result[0] = 1
	result = append(result, nonce...)
	return gcm.Seal(result, nonce, plaintext, secretAAD), nil
}

// Decrypt authenticates and decrypts a stored secret envelope.
func (key *MasterKey) Decrypt(envelope []byte) ([]byte, error) {
	gcm, err := key.gcm()
	if err != nil {
		return nil, err
	}
	if len(envelope) < 1+gcm.NonceSize()+gcm.Overhead() || envelope[0] != 1 {
		return nil, errors.New("invalid encrypted secret")
	}
	nonce := envelope[1 : 1+gcm.NonceSize()]
	plaintext, err := gcm.Open(nil, nonce, envelope[1+gcm.NonceSize():], secretAAD)
	if err != nil {
		return nil, errors.New("encrypted secret authentication failed")
	}
	return plaintext, nil
}

func (key *MasterKey) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(key.value[:])
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create AES-GCM: %w", err)
	}
	return gcm, nil
}
