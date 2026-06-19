// secret - AES-256-CBC encryption utility for pdttweb secrets
//
// Usage:
//
//	secret gen              Generate new .secret file
//	secret enc <text>       Encrypt text (SECRET env var or reads .secret)
//	secret dec <text>       Decrypt text (SECRET env var or reads .secret)
package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

const secretFile = ".secret"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "gen":
		must(generate())
	case "enc":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: secret enc <text>")
			os.Exit(1)
		}
		must(encrypt(os.Args[2]))
	case "dec":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: secret dec <text>")
			os.Exit(1)
		}
		must(decrypt(os.Args[2]))
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`secret - AES-256-CBC encryption utility

Usage:
  secret gen          Generate new .secret file
  secret enc <text>   Encrypt text
  secret dec <text>   Decrypt text

Environment:
  SECRET  Base64-encoded 48-byte key (auto-loaded from .secret if present)`)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func generate() error {
	key := make([]byte, 48)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	if err := os.WriteFile(secretFile, []byte(encoded), 0o600); err != nil {
		return err
	}
	fmt.Printf("Generated %s\n", secretFile)
	fmt.Printf("Run: export SECRET=\"$(cat %s)\"\n", secretFile)
	return nil
}

func keyIV() ([]byte, []byte, error) {
	s := os.Getenv("SECRET")
	if s == "" {
		data, err := os.ReadFile(secretFile)
		if err != nil {
			return nil, nil, fmt.Errorf("SECRET not set and .secret not found")
		}
		s = string(data)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, nil, fmt.Errorf("decode SECRET: %w", err)
	}
	if len(decoded) < 48 {
		return nil, nil, fmt.Errorf("SECRET too short")
	}
	return decoded[:32], decoded[32:48], nil
}

func encrypt(plaintext string) error {
	key, iv, err := keyIV()
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	padLen := aes.BlockSize - len(plaintext)%aes.BlockSize
	padded := make([]byte, len(plaintext)+padLen)
	copy(padded, plaintext)
	for i := len(plaintext); i < len(padded); i++ {
		padded[i] = byte(padLen)
	}
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)
	fmt.Println(base64.StdEncoding.EncodeToString(ciphertext))
	return nil
}

func decrypt(ciphertextB64 string) error {
	key, iv, err := keyIV()
	if err != nil {
		return err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(ciphertextB64))
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(ciphertext, ciphertext)
	padLen := int(ciphertext[len(ciphertext)-1])
	fmt.Println(string(ciphertext[:len(ciphertext)-padLen]))
	return nil
}
