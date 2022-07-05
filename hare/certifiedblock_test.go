package hare_test

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/hare"
	"sync"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/require"
)

// Integration Tests

func Test_WaitingForCertificate_SignaturesFromGossip(t *testing.T) {
	service, err := hare.NewBlockCertifyingService()
	require.NoError(t, err)
	store, err := hare.NewCertifiedBlockStore()
	require.NoError(t, err)

	// Wait until a block is certified:
	bID := types.RandomBlockID()
	cBlockProvider, _ := hare.NewCertifiedBlockProvider(service, store, bID)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		certCh := make(chan *hare.BlockCertificate)
		// Block until valid BlockCertificate is successfully built.
		cBlockProvider.RegisterBlockCertificateChannel(certCh)
		if cert := <-certCh; cert == nil {

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

func Test_CertifiedBlockStore_StoresValidBlocks(t *testing.T) {

}
func Test_CertifiedBlockProvider_ProvidesValidBlocks(t *testing.T) {

}
