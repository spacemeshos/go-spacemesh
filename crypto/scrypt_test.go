package crypto

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeriveKey(t *testing.T) {
	// declared in scrypt.go
	var dead = DefaultCypherParams
	var good = DefaultCypherParams
	const pass = "beagles"

	//test for hex decode error
	dead.Salt = "IJKL"
	data, err := DeriveKeyFromPassword(pass, dead)
	assert.True(t, bytes.Equal(data, []byte("")), fmt.Sprintf("hex decode error should give nil but gave %v", data))
	assert.Error(t, err, fmt.Sprint("hex decode should give error"))

	dead.SaltLen = 4
	dead.Salt = "ABCD"
	// force error in scrypt.Key
	// N must be a power of two so this should give error
	dead.N = 3
	data, err = DeriveKeyFromPassword(pass, dead)
	assert.True(t, bytes.Equal(data, []byte("")), fmt.Sprintf("scrypt.Key error should give [] but gave %v", data))
	assert.Error(t, err, fmt.Sprint("scrypt.Key should give error"))

	// test derivation without a valid set salt
	data, err = DeriveKeyFromPassword(pass, good)
	assert.Error(t, err, "expected no salt error")

	s, err := GetRandomBytes(good.SaltLen)
	assert.NoError(t, err, "failed to generate salt")
	good.Salt = hex.EncodeToString(s)

	// try good parameters and get valid result
	data, err = DeriveKeyFromPassword(pass, good)
	assert.Nil(t, err, fmt.Sprintf("scrypt good output error %v", err))
	assert.True(t, len(data) == good.DKLen, fmt.Sprintf("return value size %d, %d expected", len(data), good.DKLen))

}
