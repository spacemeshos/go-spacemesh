package svm

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mempool"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/svm/state"
)

type ProjectorMock struct {
	nonceDiff   uint64
	balanceDiff uint64
}

func (p *ProjectorMock) GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64, err error) {
	return prevNonce + p.nonceDiff, prevBalance - p.balanceDiff, nil
}

type appliedTxsMock struct{}

func (appliedTxsMock) Put(key []byte, value []byte) error { return nil }
func (appliedTxsMock) Delete(key []byte) error            { panic("implement me") }
func (appliedTxsMock) Get(key []byte) ([]byte, error)     { panic("implement me") }
func (appliedTxsMock) Has(key []byte) (bool, error)       { panic("implement me") }
func (appliedTxsMock) Close()                             { panic("implement me") }
func (appliedTxsMock) NewBatch() database.Batch           { panic("implement me") }
func (appliedTxsMock) Find(key []byte) database.Iterator  { panic("implement me") }

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

func TestHandleTransaction_ValidateOrigin(t *testing.T) {
	r := require.New(t)

	db := database.NewMemDatabase()
	lg := logtest.New(t).WithName("svm_logger")
	proc := state.NewTransactionProcessor(db, appliedTxsMock{}, &ProjectorMock{}, mempool.NewTxMemPool(), lg)
	svm := New(proc, lg)

	signer := signing.NewEdSigner()
	origin := types.BytesToAddress(signer.PublicKey().Bytes())
	svm.state.SetBalance(origin, 500)
	svm.state.SetNonce(origin, 3)

	tx := newTx(t, 3, 10, signer)
	err := svm.HandleTransaction(tx, true)
	r.NoError(err)
}

func TestHandleTransaction_DontValidateOrigin(t *testing.T) {
	r := require.New(t)

	db := database.NewMemDatabase()
	lg := logtest.New(t).WithName("svm_logger")
	proc := state.NewTransactionProcessor(db, appliedTxsMock{}, &ProjectorMock{}, mempool.NewTxMemPool(), lg)
	svm := New(proc, lg)

	signer := signing.NewEdSigner()
	origin := types.BytesToAddress(signer.PublicKey().Bytes())
	svm.state.SetBalance(origin, 300)
	svm.state.SetNonce(origin, 5)

	tx := newTx(t, 5, 10, signer)
	err := svm.HandleTransaction(tx, false)
	r.NoError(err)
}

func TestHandleTransaction_WrongNonce(t *testing.T) {
	r := require.New(t)

	db := database.NewMemDatabase()
	lg := logtest.New(t).WithName("svm_logger")
	proc := state.NewTransactionProcessor(db, appliedTxsMock{}, &ProjectorMock{}, mempool.NewTxMemPool(), lg)
	svm := New(proc, lg)

	signer := signing.NewEdSigner()
	origin := types.BytesToAddress(signer.PublicKey().Bytes())
	svm.state.SetBalance(origin, 1000)
	svm.state.SetNonce(origin, 5)

	tx := newTx(t, 2, 10, signer)
	err := svm.HandleTransaction(tx, true)
	r.Error(err, "nonce and balance validation failed; Expected: 5, Actual: 7")
}

func TestHandleTransaction_InsufficientBalance(t *testing.T) {
	r := require.New(t)

	db := database.NewMemDatabase()
	lg := logtest.New(t).WithName("svm_logger")
	proc := state.NewTransactionProcessor(db, appliedTxsMock{}, &ProjectorMock{}, mempool.NewTxMemPool(), lg)
	svm := New(proc, lg)

	signer := signing.NewEdSigner()
	origin := types.BytesToAddress(signer.PublicKey().Bytes())
	svm.state.SetBalance(origin, 5)
	svm.state.SetNonce(origin, 2)

	tx := newTx(t, 2, 10, signer)
	err := svm.HandleTransaction(tx, true)
	r.Error(err, "nonce and balance validation failed: insufficient balance")
}
