package pb

import (
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestExtractData(t *testing.T) {
	pk := p2pcrypto.NewRandomPubkey()

	assert.NotNil(t, pk)

	meta := NewProtocolMessageMetadata(pk, "EX")

	msg := &ProtocolMessage{
		Metadata: meta,
		Payload: &Payload{
			Data: &Payload_Payload{[]byte("test")},
		},
	}
	data1, err := ExtractData(msg.Payload)
	require.NoError(t, err)
	_, ok := data1.(*service.DataBytes)
	require.True(t, ok)

	msg2 := &ProtocolMessage{
		Metadata: meta,
		Payload: &Payload{
			Data: &Payload_Msg{&MessageWrapper{Type: 0, Req: true, ReqID: 0, Payload: []byte("test")}},
		},
	}
	data2, err := ExtractData(msg2.Payload)
	require.NoError(t, err)
	_, ok = data2.(*service.DataMsgWrapper)
	require.True(t, ok)

	msg3 := &ProtocolMessage{
		Metadata: meta,
		Payload:  nil,
	}

	data3, err := ExtractData(msg3.Payload)
	require.Error(t, err)
	require.Nil(t, data3)
}

func Test_NewProtocolMessageMeatadata(t *testing.T) {
	pk := p2pcrypto.NewRandomPubkey()

	assert.NotNil(t, pk)

	meta := NewProtocolMessageMetadata(pk, "EX")

	assert.NotNil(t, meta, "should be a metadata")
	assert.Equal(t, meta.Timestamp, time.Now().Unix())
	assert.Equal(t, meta.ClientVersion, config.ClientVersion)
	assert.Equal(t, meta.AuthPubkey, pk.Bytes())
	assert.Equal(t, meta.NextProtocol, "EX")
}
