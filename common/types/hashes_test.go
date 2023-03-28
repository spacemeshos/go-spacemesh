package types

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/assert"
)

func TestHash(t *testing.T) {
	msg1 := []byte("msg1")
	msg2 := []byte("msg2")
	prot1 := "prot1"
	prot2 := "prot2"

	assert.NotEqual(t, CalcMessageHash12(msg1, prot1), CalcMessageHash12(msg1, prot2))
	assert.NotEqual(t, CalcMessageHash12(msg1, prot1), CalcMessageHash12(msg2, prot1))
}

func TestConvert32_20Hash(t *testing.T) {
	msg := []byte("abcdefghijk")
	hash32 := CalcHash32(msg)
	hash20 := hash32.ToHash20()
	cHash32 := hash20.ToHash32()
	hash20b := cHash32.ToHash20()
	assert.Equal(t, hash20b, hash20)
}

func FuzzHash32Consistency(f *testing.F) {
	tester.FuzzConsistency[Hash32](f)
}

func FuzzHash32Safety(f *testing.F) {
	tester.FuzzSafety[Hash32](f)
}
