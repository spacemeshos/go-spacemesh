// Package crypto provides funcs and types used by other packages to perform crypto related ops
package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"errors"
)

// AesCTRXOR is an AES cipher following https://leanpub.com/gocrypto/read#leanpub-auto-aes-cbc .
func AesCTRXOR(key, input, nonce []byte) ([]byte, error) {
	aesBlock, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	stream := cipher.NewCTR(aesBlock, nonce)
	output := make([]byte, len(input))
	stream.XORKeyStream(output, input)
	return output, nil
}

// Pkcs7Pad returns input with padding using the pkcs7 padding spec.
func Pkcs7Pad(in []byte) []byte {
	padding := 16 - (len(in) % 16)
	for i := 0; i < padding; i++ {
		in = append(in, byte(padding))
	}
	return in
}

// Pkcs7Unpad returned in without padded data using pkcs7 padding spec.
func Pkcs7Unpad(in []byte) []byte {
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

// AddPKCSPadding Adds padding to a block of data.
func AddPKCSPadding(src []byte) []byte {
	padding := aes.BlockSize - len(src)%aes.BlockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(src, padtext...)
}

// RemovePKCSPadding Removes padding from data that was added using AddPKCSPadding.
func RemovePKCSPadding(src []byte) ([]byte, error) {
	length := len(src)
	padLength := int(src[length-1])
	if padLength > aes.BlockSize || length < aes.BlockSize {
		return nil, errors.New("invalid PKCS#7 padding")
	}

	return src[:length-padLength], nil
}
