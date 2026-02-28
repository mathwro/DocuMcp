package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"

	"github.com/documcp/documcp/internal/db"
)

// TokenStore encrypts and stores OAuth tokens in SQLite using AES-256-GCM.
type TokenStore struct {
	db  *db.Store
	key []byte // must be 32 bytes (AES-256)
}

// NewTokenStore creates a TokenStore backed by store, encrypting with key.
// key must be exactly 32 bytes for AES-256.
func NewTokenStore(store *db.Store, key []byte) *TokenStore {
	return &TokenStore{db: store, key: key}
}

// Save encrypts token and persists it for the given sourceID and provider.
func (ts *TokenStore) Save(sourceID int64, provider string, token *Token) error {
	plaintext, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}
	ciphertext, err := ts.encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("encrypt token: %w", err)
	}
	if err := ts.db.UpsertToken(sourceID, provider, ciphertext, token.ExpiresAt); err != nil {
		return fmt.Errorf("save token: %w", err)
	}
	return nil
}

// Load retrieves and decrypts the token for the given sourceID and provider.
// Returns an error (wrapping db.ErrNotFound via sql.ErrNoRows) if not found.
func (ts *TokenStore) Load(sourceID int64, provider string) (*Token, error) {
	ciphertext, err := ts.db.GetToken(sourceID, provider)
	if err != nil {
		return nil, err // preserve underlying sentinel (sql.ErrNoRows wrapped)
	}
	plaintext, err := ts.decrypt(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt token: %w", err)
	}
	var token Token
	if err := json.Unmarshal(plaintext, &token); err != nil {
		return nil, fmt.Errorf("unmarshal token: %w", err)
	}
	return &token, nil
}

func (ts *TokenStore) encrypt(plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(ts.key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func (ts *TokenStore) decrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(ts.key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}
