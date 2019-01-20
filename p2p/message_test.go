package p2p

import (
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/cryptoBox"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func Test_NewProtocolMessageMeatadata(t *testing.T) {
	_, pk, _ := cryptoBox.GenerateKeyPair()

	assert.NotNil(t, pk)

	meta := NewProtocolMessageMetadata(pk, "EX")

	assert.NotNil(t, meta, "should be a metadata")
	assert.Equal(t, meta.Timestamp, time.Now().Unix())
	assert.Equal(t, meta.ClientVersion, config.ClientVersion)
	assert.Equal(t, meta.AuthPubkey, pk.Bytes())
	assert.Equal(t, meta.NextProtocol, "EX")
}
