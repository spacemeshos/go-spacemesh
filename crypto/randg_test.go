package crypto

import (
	"bytes"
	"fmt"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

func TestGetRandomBytesToBuffer(t *testing.T) {
	n := 32
	tb := make([]byte, n)
	eb := make([]byte, n)
	// pass in a zero
	err := GetRandomBytesToBuffer(0, tb)
	assert.Error(t, err, "zero parameter should give error")
	// deliberately too large n parameter
	err = GetRandomBytesToBuffer(n+1, tb)
	assert.Error(t, err, "too large parameter should give error")
	// call with good parameters
	err = GetRandomBytesToBuffer(n, tb)
	assert.Equal(t, err, nil, fmt.Sprintf("unexpected error %v", err))
	assert.False(t, bytes.Equal(tb, eb), "null data from GetRandomBytes")

}

func TestGetRandomBytes(t *testing.T) {
	n := 32
	eb := make([]byte, n)
	// pass in a zero
	tb, err := GetRandomBytes(0)
	assert.Error(t, err, "zero parameter should give error")
	// call with good parameters
	tb, err = GetRandomBytes(n)
	assert.Equal(t, err, nil, fmt.Sprintf("unexpected error %v", err))
	assert.False(t, bytes.Equal(tb, eb), "null data from GetRandomBytes")
}

func TestRandomUserPort(t *testing.T) {
	x := GetRandomUserPort()
	assert.True(t, x > 1023, "unexpected small value")
	assert.True(t, x < 49151, "unexpected large value")
}

func TestGetRandomUInt32(t *testing.T) {
	var max = uint32(16384)
	x := GetRandomUInt32(max)
	assert.True(t, unsafe.Sizeof(x) == 4, "unexpected GetRandomUInt32 failure")
}
