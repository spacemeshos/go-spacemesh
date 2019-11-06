package crypto

import (
	"crypto/aes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAesCTRXORandPkcs7Pad(t *testing.T) {
	const msg = "does not matter"
	msgData := []byte(msg)

	dudtestkey := make([]byte, 17, 17)
	var nb = []byte(nil)
	// dudtestkey is initially not a multiple of 16 so nil is returned
	_, err := AesCTRXOR(dudtestkey, msgData, nb)
	assert.NotNil(t, err, fmt.Sprintf("incorrect length should error <%v>", err))
	assert.Equal(t, fmt.Sprintf("%s", err), "crypto/aes: invalid key size 17", fmt.Sprintf("incorrect length should error (%s)", err))
	// Pkcs7Pad makes a good lengthed key from the bad one
	happytestkey := Pkcs7Pad(dudtestkey)
	assert.Equal(t, len(happytestkey), 32, fmt.Sprintf("Pkcs7Pad incorrect length %d", len(happytestkey)))
	// now call AesCTRXOR with good parameters and get a nice return value
	happynonce := make([]byte, 16, 16)
	happy, err := AesCTRXOR(happytestkey, msgData, happynonce)
	// check no error
	assert.Nil(t, err, "should be happy aesctr err")
	// check something like data is in return value
	assert.NotNil(t, happy, "should be happy aesctr data")
}

func TestPkcs7Unpad(t *testing.T) {
	zerolength := []byte("")
	// check that zero length payload gives nil
	rv := Pkcs7Unpad(zerolength)
	assert.Equal(t, len(rv), 0, fmt.Sprintf("zero length payload should return nil but is %v", rv))
	// 26 letters + 7 dots
	wrongsize := []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ.......")
	// trigger padding > blocksize, expect nil
	wrongsize[len(wrongsize)-1] = byte(aes.BlockSize + 1)
	rv = Pkcs7Unpad(wrongsize)
	assert.Equal(t, len(rv), 0, fmt.Sprintf("padding > blocksize should return nil but is %v", rv))
	// trigger padding ==0 , expect nil
	wrongsize[len(wrongsize)-1] = 0
	rv = Pkcs7Unpad(wrongsize)
	assert.Equal(t, len(rv), 0, fmt.Sprintf("padding == 0 should return nil but is %v", rv))
	// trigger non padding byte found during reduction
	pad := 7
	for i := len(wrongsize) - 1; i > len(wrongsize)-pad; i-- {
		wrongsize[i] = byte(pad)
	}
	rv = Pkcs7Unpad(wrongsize)
	assert.Equal(t, len(rv), 0, fmt.Sprintf("non padding byte found should return nil but is %v", rv))
	// actually remove some padding and get a proper good result
	for i := len(wrongsize) - 1; i > len(wrongsize)-pad-1; i-- {
		wrongsize[i] = byte(pad)
	}
	rightsize := Pkcs7Unpad(wrongsize)
	assert.True(t, len(rightsize) > 0 && len(rightsize) == 26, fmt.Sprintf("should not be sized %d", len(rightsize)))

}
func TestPKCSPadding(t *testing.T) {
	// 26 letters + 7 dots is 33 bytes
	wrongsize := []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ.......")
	// should pad up to a size of a multiple of 16
	padout := AddPKCSPadding(wrongsize)
	assert.True(t, len(padout) == 48, fmt.Sprintf("should not be sized %d", len(padout)))
	unpad, err := RemovePKCSPadding(padout)
	assert.True(t, len(unpad) == len(wrongsize), fmt.Sprintf("should not be sized %d", len(padout)))
	assert.Nil(t, err, fmt.Sprintf("error from RemovePKCSPadding %d", err))
	// force error by setting pad length incorrectly
	breaklength := padout
	breaklength[len(breaklength)-1] = byte(17)
	breaklengthrv, errb := RemovePKCSPadding(padout)
	assert.Equal(t, len(breaklengthrv), 0, fmt.Sprintf("should not be sized %d", len(breaklengthrv)))
	assert.NotNil(t, errb, fmt.Sprintf("no error from RemovePKCSPadding %d", errb))
}
