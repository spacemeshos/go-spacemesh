package blockcerts_test

import (
	"context"
	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-spacemesh/blockcerts"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/hare/mocks"
	"sync"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/require"
)

func Test_ProducesValidBlockSignature_ChosenByOracle(t *testing.T) {
	// Arrange
	mockCtrler := gomock.NewController(t)
	mockRolacle := mocks.NewMockRolacle(mockCtrler)
	config := blockcerts.HareTerminationConfig{
		CommitteeSize: 800,
	}

	hareTerminations := make(chan hare.TerminationOutput, 50)

	// Act
	//certServ, err := blockcerts.NewBlockCertifyingService(
	//	mockRolacle, config,
	//	hareTerminations, )

	// Assert
}

func Test_WaitingForCertificate_SignaturesFromGossip(t *testing.T) {
	service, err := blockcerts.NewBlockCertifyingService()
	require.NoError(t, err)
	store, err := blockcerts.NewCertifiedBlockStore()
	require.NoError(t, err)

	// Wait until a block is certified:
	bID := types.RandomBlockID()
	cBlockProvider, _ := blockcerts.NewCertifiedBlockProvider(service, store, bID)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		certCh := make(chan *blockcerts.BlockCertificate)
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
