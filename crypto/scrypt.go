package crypto

import (
	"encoding/hex"
	"errors"
	"golang.org/x/crypto/scrypt"
)

type KDParams struct {
	N       int    `json:"n"`
	R       int    `json:"r"`
	P       int    `json:"p"`
	SaltLen int    `json:"saltLen"`
	DKLen   int    `json:"dkLen"`
	Salt    string `json:"salt"` // hex encoded
}

var DefaultCypherParams = KDParams{N: 262144, R: 8, P: 1, SaltLen: 16, DKLen: 32}

// Derive a key from password using provided Cipher params
func DeriveKeyFromPassword(password string, p KDParams) ([]byte, error) {

	if len(p.Salt) == 0 {
		return nil, errors.New("Invalid salt length param")
	}

	salt, err := hex.DecodeString(p.Salt)
	if err != nil {
		return nil, err
	}

	if len(salt) != p.SaltLen {
		return nil, errors.New("missing salt")
	}

	dkData, err := scrypt.Key([]byte(password), salt, p.N, p.R, p.P, p.DKLen)
	if err != nil {
		return nil, err
	}

	return dkData, nil
}
