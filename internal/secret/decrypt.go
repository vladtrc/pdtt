package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"os"
	"strings"
)

// Decrypt decrypts an AES-256-CBC base64-encoded ciphertext using a
// base64-encoded 48-byte secret (32 key + 16 IV) from the SECRET env var.
func Decrypt(encoded string) (string, error) {
	secretEnv := os.Getenv("SECRET")
	if secretEnv == "" {
		return "", errors.New("SECRET environment variable not set")
	}
	return DecryptWithKey(encoded, secretEnv)
}

func DecryptWithKey(encoded, secretB64 string) (string, error) {
	secretBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(secretB64))
	if err != nil {
		return "", err
	}
	if len(secretBytes) < 48 {
		return "", errors.New("secret too short: need 48 bytes")
	}
	key, iv := secretBytes[:32], secretBytes[32:48]

	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", err
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return "", errors.New("ciphertext not aligned to block size")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(ciphertext, ciphertext)

	padLen := int(ciphertext[len(ciphertext)-1])
	if padLen == 0 || padLen > aes.BlockSize || padLen > len(ciphertext) {
		return "", errors.New("invalid PKCS7 padding")
	}
	return string(ciphertext[:len(ciphertext)-padLen]), nil
}
