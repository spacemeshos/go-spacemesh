package hare

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const numOfClients = 100

func TestMockHashOracle_Register(t *testing.T) {
	oracle := newMockHashOracle(numOfClients)
	oracle.Register(types.RandomNodeID().String())
	oracle.Register(types.RandomNodeID().String())
	assert.Equal(t, 2, len(oracle.clients))
}

func TestMockHashOracle_Unregister(t *testing.T) {
	oracle := newMockHashOracle(numOfClients)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	oracle.Register(signer.PublicKey().String())
	assert.Equal(t, 1, len(oracle.clients))
	oracle.Unregister(signer.PublicKey().String())
	assert.Equal(t, 0, len(oracle.clients))
}

func TestMockHashOracle_Concurrency(t *testing.T) {
	oracle := newMockHashOracle(numOfClients)
	c := make(chan types.NodeID, 1000)

	var eg errgroup.Group
	eg.Go(func() error {
		for i := 0; i < 500; i++ {
			id := types.RandomNodeID()
			oracle.Register(id.String())
			c <- id
		}
		return nil
	})

	eg.Go(func() error {
		for i := 0; i < 400; i++ {
			id := <-c
			oracle.Unregister(id.String())
		}
		return nil
	})

	assert.NoError(t, eg.Wait())
	assert.Equal(t, len(oracle.clients), 100)
}

func TestMockHashOracle_Role(t *testing.T) {
	oracle := newMockHashOracle(numOfClients)
	for i := 0; i < numOfClients; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		oracle.Register(signer.PublicKey().String())
	}

	committeeSize := 20
	counter := 0
	for i := 0; i < numOfClients; i++ {
		res, _ := oracle.eligible(context.Background(), types.LayerID{}, 1, committeeSize, types.RandomNodeID(), types.RandomBytes(4))
		if res {
			counter++
		}
	}

	if counter*3 < committeeSize { // allow only deviation
		t.Errorf("Comity size error. Expected: %v Actual: %v", committeeSize, counter)
		t.Fail()
	}
}

func TestMockHashOracle_calcThreshold(t *testing.T) {
	oracle := newMockHashOracle(2)
	oracle.Register(types.RandomNodeID().String())
	oracle.Register(types.RandomNodeID().String())
	assert.Equal(t, uint32(math.MaxUint32/2), oracle.calcThreshold(1))
	assert.Equal(t, uint32(math.MaxUint32), oracle.calcThreshold(2))
}
