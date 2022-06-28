package hare

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// CertifiedBlockService is a thread-safe service which participates in the building
// of certified blocks.
type BlockCertifiyingService struct {
}

func (bcs *BlockCertifiyingService) Start(ctx context.Context) error {
	return fmt.Errorf("not yet implemented")
}

func NewBlockCertifiyingService() (*BlockCertifiyingService, error) {
	return nil, fmt.Errorf("not yet implemented")
}

type CertifiedBlockStore struct {
}

func (cbs *CertifiedBlockStore) StoreCertifiedBlock() error {
	return fmt.Errorf("not yet implemented")
}

type CertifiedBlockProvider struct {
}

func NewCertifiedBlockProvider(s BlockCertifiyingService) (*CertifiedBlockProvider, error) {
	return nil, fmt.Errorf("not yet implemented")
}

func (cbp *CertifiedBlockProvider) WaitForCertifiedBlock() (<-chan types.BlockID, error) {
	return nil, fmt.Errorf("not implemented yet")
}

func (cbp *CertifiedBlockProvider) GetCertifiedBlock() {

}

// Implementation details below ----

type terminationSignature struct {
	eligibilityProof [64]byte
	blockSignature   [80]byte
}

type certifiedBlock struct {
	blockID               types.BlockID
	terminationSignatures []terminationSignature
}

func (cbs *CertifiedBlockStore) storeBlockSignature(blockID types.BlockID, sig terminationSignature) error {
	return fmt.Errorf("not yet implemented")
}
