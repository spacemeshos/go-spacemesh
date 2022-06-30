package hare

import (
	"context"
	"sync"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/require"
)

// Integration Tests

func Test_WaitingForCertificate_SignaturesFromGossip(t *testing.T) {
	service, err := NewBlockCertifyingService()
	require.NoError(t, err)
	store, err := NewCertifiedBlockStore()
	require.NoError(t, err)

	// Wait until a block is certified:
	bID := types.RandomBlockID()
	cBlockProvider, _ := NewCertifiedBlockProvider(service, store, bID)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Block until valid BlockCertificate is successfully built.
		cert := <-cBlockProvider.GetBlockCertificateChannel()
		if cert != nil {

		}
		// Do something with the block certificate from the channel or just block and get
		// notified once a valid certificate is successfully built.
	}()

	err = service.Start(context.Background())
	require.NoError(t, err)
	wg.Wait()
}

func Test_WaitingForCertificate_SignaturesAddedToStore(t *testing.T) {

}

func Test_CertifiedBlockStore_StoresBlock(t *testing.T) {

}
func Test_CertifiedBlockProvider_ProvidesBlock(t *testing.T) {

}
