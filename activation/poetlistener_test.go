package activation

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/lp2p"
	"github.com/spacemeshos/go-spacemesh/lp2p/pubsub"
)

type PoetDbIMock struct {
	lock          sync.Mutex
	validationErr error
}

func (p *PoetDbIMock) Validate(proof types.PoetProof, poetID []byte, roundID string, signature []byte) error {
	p.lock.Lock()
	defer p.lock.Unlock()
	return p.validationErr
}

func (p *PoetDbIMock) SetErr(err error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	p.validationErr = err
}

func (p *PoetDbIMock) storeProof(proofMessage *types.PoetProofMessage) error { return nil }

func TestNewPoetListener(t *testing.T) {
	lg := logtest.New(t)
	poetDb := PoetDbIMock{}
	listener := NewPoetListener(&poetDb, lg)

	msg, err := types.InterfaceToBytes(&types.PoetProofMessage{})
	require.NoError(t, err)
	require.Equal(t, pubsub.ValidationAccept,
		listener.HandlePoetProofMessage(context.TODO(), lp2p.Peer("test"), msg))

	poetDb.SetErr(fmt.Errorf("bad poet message"))
	require.Equal(t, pubsub.ValidationIgnore,
		listener.HandlePoetProofMessage(context.TODO(), lp2p.Peer("test"), msg))
}
