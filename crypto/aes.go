package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
)

var (
	aesDecryptionError = errors.New("Aes decryption error")
)

// AES cipher following https://leanpub.com/gocrypto/read#leanpub-auto-aes-cbc

func AesCTREncrypt(key, clearText, nonce []byte) ([]byte, error) {
	aesBlock, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	stream := cipher.NewCTR(aesBlock, nonce)
	cipherText := make([]byte, len(clearText))
	stream.XORKeyStream(cipherText, clearText)

	return cipherText, nil
}

func AesCBCDecrypt(key, cipherText, iv []byte) ([]byte, error) {

	aesBlock, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	d := cipher.NewCBCDecrypter(aesBlock, iv)
	paddedPlainText := make([]byte, len(cipherText))
	d.CryptBlocks(paddedPlainText, cipherText)
	plaintext := unpad(paddedPlainText)
	if plaintext == nil {
		return nil, aesDecryptionError
	}

	return plaintext, nil
}

// pkcs7 padding
func pkcs7Pad(in []byte) []byte {
	padding := 16 - (len(in) % 16)
	for i := 0; i < padding; i++ {
		in = append(in, byte(padding))
	}
	return in
}

// pkcs7 unpadding
func unpad(in []byte) []byte {
	if len(in) == 0 {
		return nil
	}

	padding := in[len(in)-1]
	if int(padding) > len(in) || padding > aes.BlockSize {
		return nil
	} else if padding == 0 {
		return nil
	}

	for i := len(in) - 1; i > len(in)-int(padding)-1; i-- {
		if in[i] != padding {
			return nil
		}
	}
	return in[:len(in)-int(padding)]
}
