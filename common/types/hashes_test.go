package types

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvert32_20Hash(t *testing.T) {
	msg := []byte("abcdefghijk")
	hash32 := CalcHash32(msg)
	hash20 := hash32.ToHash20()
	cHash32 := hash20.ToHash32()
	hash20b := cHash32.ToHash20()
	assert.Equal(t, hash20b, hash20)
}

func TestHash20(t *testing.T) {
	require.Equal(t, CalcHash20([]byte("hello")), CalcHash32([]byte("hello")).ToHash20())
}

func TestHash30ToBytes(t *testing.T) {
	b := []byte("0123456789abcdef_hello_world_how_are_you") // 40 bytes
	var h Hash32
	h.SetBytes(b)
	require.Equal(t, []byte("89abcdef_hello_world_how_are_you"), h.Bytes())
}

func TestHash20ToBytes(t *testing.T) {
	b := []byte("0123456789abcdef_hello_world_how_are_you") // 40 bytes
	var h Hash20
	h.SetBytes(b)
	require.Equal(t, []byte("lo_world_how_are_you"), h.Bytes())
}

func FuzzHash32Consistency(f *testing.F) {
	tester.FuzzConsistency[Hash32](f)
}

func FuzzHash32Safety(f *testing.F) {
	tester.FuzzSafety[Hash32](f)
}
