package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

type FileVaultBackend struct {
	mu       sync.RWMutex
	filepath string
	key      []byte // AES-256 key
	secrets  map[string]string
}

func NewFileVaultBackend(filepath string) (*FileVaultBackend, error) {
	// Resolve key
	rawKey := os.Getenv("HAG_VAULT_KEY")
	if rawKey == "" {
		return nil, fmt.Errorf(
			"HAG_VAULT_KEY environment variable is required for file vault backend; " +
				"set a strong random key (e.g. openssl rand -base64 32)")
	}

	// Derivate 32-byte key from raw key using SHA-256
	h := sha256.New()
	h.Write([]byte(rawKey))
	key := h.Sum(nil)

	b := &FileVaultBackend{
		filepath: filepath,
		key:      key,
		secrets:  make(map[string]string),
	}

	// Load existing secrets if file exists
	if _, err := os.Stat(filepath); err == nil {
		if err := b.load(); err != nil {
			return nil, fmt.Errorf("failed to load vault file: %w", err)
		}
	}

	return b, nil
}

func (b *FileVaultBackend) load() error {
	ciphertext, err := os.ReadFile(b.filepath)
	if err != nil {
		return err
	}

	if len(ciphertext) == 0 {
		b.secrets = make(map[string]string)
		return nil
	}

	plaintext, err := decrypt(ciphertext, b.key)
	if err != nil {
		return fmt.Errorf("decryption failed: %w", err)
	}

	var secrets map[string]string
	if err := json.Unmarshal(plaintext, &secrets); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	b.secrets = secrets
	return nil
}

func (b *FileVaultBackend) save() error {
	plaintext, err := json.Marshal(b.secrets)
	if err != nil {
		return err
	}

	ciphertext, err := encrypt(plaintext, b.key)
	if err != nil {
		return err
	}

	return os.WriteFile(b.filepath, ciphertext, 0600)
}

func (b *FileVaultBackend) Resolve(ref string) (string, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	const prefix = "vault://"
	if !strings.HasPrefix(ref, prefix) {
		return "", fmt.Errorf("invalid vault reference: %s", ref)
	}
	path := strings.TrimPrefix(ref, prefix)

	val, ok := b.secrets[path]
	if !ok {
		return "", fmt.Errorf("secret not found: %s", path)
	}
	return val, nil
}

func (b *FileVaultBackend) Set(path, value string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.secrets[path] = value
	return b.save()
}

func (b *FileVaultBackend) Delete(path string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.secrets[path]; !ok {
		return fmt.Errorf("secret not found: %s", path)
	}

	delete(b.secrets, path)
	return b.save()
}

func (b *FileVaultBackend) List() ([]string, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	keys := make([]string, 0, len(b.secrets))
	for k := range b.secrets {
		keys = append(keys, k)
	}
	return keys, nil
}

// AES-GCM helpers

func encrypt(plaintext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// Sealed data is nonce + ciphertext
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decrypt(ciphertext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce := ciphertext[:nonceSize]
	actualCiphertext := ciphertext[nonceSize:]

	return gcm.Open(nil, nonce, actualCiphertext, nil)
}
