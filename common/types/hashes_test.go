package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConvert32_20Hash(t *testing.T) {
	msg := []byte("abcdefghijk")
	hash32 := CalcHash32(msg)
	hash20 := hash32.ToHash20()
	cHash32 := hash20.ToHash32()
	hash20b := cHash32.ToHash20()
	assert.Equal(t, hash20b, hash20)
}
