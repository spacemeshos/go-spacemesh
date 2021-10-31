package svm

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type SvmAPISuite struct {
	suite.Suite
	svm *SVM
}

func (s *SvmAPISuite) SetupTest() {
	logger := logtest.New(s.T()).WithName("svm_api_logger")
	s.svm = New(s.svm.state, logger)
}

func createTransaction(t *testing.T, nonce uint64, destination types.Address, amount, fee uint64, signer *signing.EdSigner) *types.Transaction {
	tx, err := types.NewSignedTx(nonce, destination, amount, 100, fee, signer)
	assert.NoError(t, err)
	return tx
}

func newTx(t *testing.T, nonce, totalAmount uint64, signer *signing.EdSigner) *types.Transaction {
	feeAmount := uint64(1)
	rec := types.Address{byte(rand.Int()), byte(rand.Int()), byte(rand.Int()), byte(rand.Int())}
	return createTransaction(t, nonce, rec, totalAmount-feeAmount, feeAmount, signer)
}
