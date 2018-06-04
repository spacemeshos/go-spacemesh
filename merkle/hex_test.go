package merkle

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHexFuncs(t *testing.T) {

	s := "aF3ef"
	i, ok := fromHexChar(s[0])
	assert.True(t, ok, "expected hex char")
	assert.True(t, i == 0xa, fmt.Sprintf("expected 10 hex: %d", i))

	i, ok = fromHexChar(s[1])
	assert.True(t, ok, "expected hex char")
	assert.True(t, i == 0xf, fmt.Sprintf("expected 10 hex: %d", i))

	s1 := "0a9bf3a3eba"
	s2 := "0a9bf3a3ebaffff"
	s3 := commonPrefix(s1, s2)
	assert.Equal(t, s1, s3, "unexpected suffix")

	l := lenPrefix(s1, s2)
	assert.Equal(t, l, len(s1), "unexpected length")

	s1 = "f0a9bf3a3eba"
	s2 = "0a9bf3a3ebaffff"
	s3 = commonPrefix(s1, s2)
	assert.True(t, len(s3) == 0, "unexpected suffix length")

}
