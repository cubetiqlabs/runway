package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 2
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// ParseKey32 decodes a 32-byte encryption key from base64, hex, or derives via SHA-256 if >= 32 bytes.
func ParseKey32(raw string) ([]byte, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("encryption key is empty")
	}
	if b, err := base64.StdEncoding.DecodeString(trimmed); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.RawURLEncoding.DecodeString(trimmed); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := hex.DecodeString(trimmed); err == nil && len(b) == 32 {
		return b, nil
	}
	if len(trimmed) < 32 {
		return nil, errors.New("encryption key must be 32-byte base64/hex or at least 32 characters")
	}
	sum := sha256.Sum256([]byte(trimmed))
	return sum[:], nil
}

// EncryptAESGCM encrypts plaintext using AES-256-GCM with a random 12-byte nonce.
func EncryptAESGCM(key32 []byte, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key32)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("read nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

// DecryptAESGCM decrypts AES-256-GCM ciphertext produced by EncryptAESGCM.
func DecryptAESGCM(key32 []byte, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key32)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	ns := aead.NonceSize()
	if len(ciphertext) < ns {
		return nil, errors.New("ciphertext too short")
	}
	nonce, body := ciphertext[:ns], ciphertext[ns:]
	plain, err := aead.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt gcm: %w", err)
	}
	return plain, nil
}

// HashPassword hashes a plaintext password with Argon2id and returns an encoded PHC string.
func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", errors.New("password must be at least 8 characters")
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonTime,
		argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword verifies a plaintext password against a PHC-encoded Argon2id hash in constant time.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	computed := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(expectedHash)))
	return subtle.ConstantTimeCompare(computed, expectedHash) == 1
}

// GenerateRandomToken produces a URL-safe random token with nBytes of entropy.
func GenerateRandomToken(nBytes int) (string, error) {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashTokenSHA256 computes a hex-encoded SHA-256 digest of a bearer/session token.
func HashTokenSHA256(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// VerifyGitHubSignature256 performs constant-time HMAC-SHA256 verification on raw webhook bytes.
func VerifyGitHubSignature256(payload []byte, signatureHeader, secret string) bool {
	if secret == "" || !strings.HasPrefix(signatureHeader, "sha256=") {
		return false
	}
	sigHex := strings.TrimPrefix(signatureHeader, "sha256=")
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil || len(sigBytes) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := mac.Sum(nil)
	return hmac.Equal(sigBytes, expected)
}
