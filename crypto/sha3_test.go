package crypto

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSha256(t *testing.T) {

	// test vectors keys and values MUST be valid hex strings
	// source: https://www.di-mgt.com.au/sha_testvectors.html#testvectors
	testVectors := map[string]string{

		"":       "a7ffc6f8bf1ed76651c14756a061d662f580ff4de43b49fa82d80a4b80f8434a",
		"616263": "3a985da74fe225b2045c172d6bd390bd855f086e3e9d525b46bfe24511431532",
		"6162636462636465636465666465666765666768666768696768696A68696A6B696A6B6C6A6B6C6D6B6C6D6E6C6D6E6F6D6E6F706E6F7071":                                                                                                                 "41c0dba2a9d6240849100376a8235e2c82e1b9998a999e21db32dd97496d3376",
		"61626364656667686263646566676869636465666768696a6465666768696a6b65666768696a6b6c666768696a6b6c6d6768696a6b6c6d6e68696a6b6c6d6e6f696a6b6c6d6e6f706a6b6c6d6e6f70716b6c6d6e6f7071726c6d6e6f707172736d6e6f70717273746e6f707172737475": "916f6061fe879741ca6469b43971dfdb28b1a32dc36cb3254e812be27aad1d18",
	}

	for k, v := range testVectors {
		data, err := hex.DecodeString(k)
		assert.NoError(t, err, "invalid input data")
		result := Sha256(data)
		expected, err := hex.DecodeString(v)
		assert.NoError(t, err, "invalid hex string %s", v)
		assert.True(t, bytes.Equal(result, expected), "unexpected result")
	}
}
