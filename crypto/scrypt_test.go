package crypto

import (
	"bytes"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/assert"
	"testing"
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
	assert.Err(t, err, fmt.Sprint("hex decode should give error"))

	dead.SaltLen = 4
	dead.Salt = "ABCD"
	// force error in scrypt.Key
	// N must be a power of two so this should give error
	dead.N = 3
	data, err = DeriveKeyFromPassword(pass, dead)
	assert.True(t, bytes.Equal(data, []byte("")), fmt.Sprintf("scrypt.Key error should give [] but gave %v", data))
	assert.Err(t, err, fmt.Sprint("scrypt.Key should give error"))

	// try good parameters and get valid result
	data, err = DeriveKeyFromPassword(pass, good)
	assert.Nil(t, err, fmt.Sprintf("scrypt good output error %v", err))
	assert.True(t, len(data) == good.DKLen, fmt.Sprintf("return value size %d, %d expected", len(data), good.DKLen))

}
