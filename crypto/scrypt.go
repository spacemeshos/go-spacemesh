package crypto

import (
	"encoding/hex"
	"errors"
	"fmt"

	"golang.org/x/crypto/scrypt"
)

// KDParams defines key derivation scheme params.
type KDParams struct {
	N       int    `json:"n"`
	R       int    `json:"r"`
	P       int    `json:"p"`
	SaltLen int    `json:"saltLen"`
	DKLen   int    `json:"dkLen"`
	Salt    string `json:"salt"` // hex encoded
}

// DefaultCypherParams used for key derivation by the app.
var DefaultCypherParams = KDParams{N: 262144, R: 8, P: 1, SaltLen: 16, DKLen: 32}

// DeriveKeyFromPassword derives a key from password using the provided KDParams params.
func DeriveKeyFromPassword(password string, p KDParams) ([]byte, error) {
	if len(p.Salt) == 0 {
		return nil, errors.New("invalid salt length param")
	}

	salt, err := hex.DecodeString(p.Salt)
	if err != nil {
		return nil, fmt.Errorf("decode salt: %w", err)
	}

	if len(salt) != p.SaltLen {
		return nil, errors.New("missing salt")
	}

	dkData, err := scrypt.Key([]byte(password), salt, p.N, p.R, p.P, p.DKLen)
	if err != nil {
		return nil, fmt.Errorf("derive key from password: %w", err)
	}

	return dkData, nil
}
