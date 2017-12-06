package crypto

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"golang.org/x/crypto/scrypt"
)

type CipherParams struct {
	N       int
	R       int
	P       int
	SaltLen int
	DKLen   int
}

type CipherOutput struct {
	Salt []byte
	DK   []byte
}

type ValidatePasswordParams struct {
	CipherParams
	CipherOutput
}

var DefaultCypherParams = CipherParams{N: 262144, R: 8, P: 1, SaltLen: 16, DKLen: 32}

// Derive a key from password using provided Cipher params
func DeriveKeyFromPassword(password string, p CipherParams) (*CipherOutput, error) {

	salt, err := GetRandomBytes(p.SaltLen)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Failed to generate entropy: %v", err))
	}

	dkData, err := scrypt.Key([]byte(password), salt, p.N, p.R, p.P, p.DKLen)
	if err != nil {
		return nil, err
	}

	return &CipherOutput{Salt: salt, DK: dkData}, nil
}

// Validate password
func ValidatePassword(p ValidatePasswordParams, password string) error {

	// scrypt the cleartext password with the same parameters and salt
	dk, err := scrypt.Key([]byte(password), []byte(p.Salt), p.N, p.R, p.P, p.DKLen)
	if err != nil {
		return err
	}

	// Constant time comparison
	if subtle.ConstantTimeCompare(p.DK, dk) == 1 {
		return nil
	}

	return errors.New("Password mismatch")
}
