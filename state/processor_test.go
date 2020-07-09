package state

import (
	crand "crypto/rand"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"math/big"
	"math/rand"
	"testing"
)

type ProcessorStateSuite struct {
	suite.Suite
	db        *database.MemDatabase
	processor *TransactionProcessor
	projector *ProjectorMock
}

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

func (s *ProcessorStateSuite) SetupTest() {
	lg := log.New("proc_logger", "", "")
	s.db = database.NewMemDatabase()
	s.projector = &ProjectorMock{}
	s.processor = NewTransactionProcessor(s.db, appliedTxsMock{}, s.projector, NewTxMemPool(), lg)
}

func createAccount(state *TransactionProcessor, addr types.Address, balance int64, nonce uint64) *Object {
	obj1 := state.GetOrNewStateObj(addr)
	obj1.AddBalance(big.NewInt(balance))
	obj1.SetNonce(nonce)
	state.updateStateObj(obj1)
	return obj1
}

func createTransaction(t *testing.T, nonce uint64, destination types.Address, amount, fee uint64, signer *signing.EdSigner) *types.Transaction {
	tx, err := types.NewSignedTx(nonce, destination, amount, 100, fee, signer)
	assert.NoError(t, err)
	return tx
}

func (s *ProcessorStateSuite) TestTransactionProcessor_ApplyTransaction() {
	//test happy flow
	//test happy flow with underlying structures
	//test insufficient funds
	//test wrong nonce
	//test account doesn't exist
	signerBuf := []byte("22222222222222222222222222222222")
	signerBuf = append(signerBuf, []byte{
		94, 33, 44, 9, 128, 228, 179, 159, 192, 151, 33, 19, 74, 160, 33, 9,
		55, 78, 223, 210, 96, 192, 211, 208, 60, 181, 1, 200, 214, 84, 87, 169,
	}...)
	signer, err := signing.NewEdSignerFromBuffer(signerBuf)
	assert.NoError(s.T(), err)
	obj1 := createAccount(s.processor, SignerToAddr(signer), 21, 0)
	obj2 := createAccount(s.processor, toAddr([]byte{0x01, 02}), 1, 10)
	createAccount(s.processor, toAddr([]byte{0x02}), 44, 0)
	s.processor.Commit()

	transactions := []*types.Transaction{
		createTransaction(s.T(), obj1.Nonce(), obj2.address, 1, 5, signer),
	}

	failed, err := s.processor.ApplyTransactions(1, transactions)
	assert.NoError(s.T(), err)
	assert.True(s.T(), failed == 0)

	got := string(s.processor.Dump())

	assert.Equal(s.T(), uint64(15), s.processor.GetBalance(obj1.address))
	assert.Equal(s.T(), uint64(1), s.processor.GetNonce(obj1.address))

	want := `{
	"root": "6de6ffd7eda4c1aa4de66051e4ad05afc1233e089f9e9afaf8174a4dc483fa57",
	"accounts": {
		"0000000000000000000000000000000000000002": {
			"balance": "44",
			"nonce": 0
		},
		"0000000000000000000000000000000000000102": {
			"balance": "2",
			"nonce": 10
		},
		"4aa02109374edfd260c0d3d03cb501c8d65457a9": {
			"balance": "15",
			"nonce": 1
		}
	}
}`
	if got != want {
		s.T().Errorf("dump mismatch:\ngot: %s\nwant: %s\n", got, want)
	}
}

func SignerToAddr(signer *signing.EdSigner) types.Address {
	return types.BytesToAddress(signer.PublicKey().Bytes())
}

/*func (s *ProcessorStateSuite) TestTransactionProcessor_ApplyTransaction_DoubleTrans() {
	//test happy flow
	//test happy flow with underlying structures
	//test insufficient funds
	//test wrong nonce
	//test account doesn't exist
	obj1 := createAccount(s.processor, []byte{0x01}, 21, 0)
	obj2 := createAccount(s.processor, []byte{0x01, 02}, 1, 10)
	createAccount(s.processor, []byte{0x02}, 44, 0)
	s.processor.Commit(false)

	transactions := Transactions{
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 1),
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 1),
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 1),
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 1),
	}

	failed, err := s.processor.ApplyTransactions(1, transactions)
	assert.NoError(s.T(), err)
	assert.True(s.T(), failed == 0)

	got := string(s.processor.Dump())

	assert.Equal(s.T(), big.NewInt(20), s.processor.GetBalance(obj1.address))

	want := `{
	"root": "7ed462059ad2df6754b5aa1f3d8a150bb9b0e1c4eb50b6217a8fc4ecbec7fb28",
	"accounts": {
		"0000000000000000000000000000000000000001": {
			"balance": "20",
			"nonce": 1
		},
		"0000000000000000000000000000000000000002": {
			"balance": "44",
			"nonce": 0
		},
		"0000000000000000000000000000000000000102": {
			"balance": "2",
			"nonce": 10
		}
	}
}`
	if got != want {
		s.T().Errorf("dump mismatch:\ngot: %s\nwant: %s\n", got, want)
	}
}*/

func (s *ProcessorStateSuite) TestTransactionProcessor_ApplyTransaction_Errors() {
	signer1 := signing.NewEdSigner()
	obj1 := createAccount(s.processor, SignerToAddr(signer1), 21, 0)
	obj2 := createAccount(s.processor, toAddr([]byte{0x01, 02}), 1, 10)
	createAccount(s.processor, toAddr([]byte{0x02}), 44, 0)
	s.processor.Commit()

	transactions := []*types.Transaction{
		createTransaction(s.T(), obj1.Nonce(), obj2.address, 1, 5, signer1),
	}

	failed, err := s.processor.ApplyTransactions(1, transactions)
	assert.NoError(s.T(), err)
	assert.True(s.T(), failed == 0)

	err = s.processor.ApplyTransaction(createTransaction(s.T(), 0, obj2.address, 1, 5, signer1), 0)
	assert.Error(s.T(), err)
	assert.Equal(s.T(), err.Error(), errNonce)

	err = s.processor.ApplyTransaction(createTransaction(s.T(), obj1.Nonce(), obj2.address, 21, 5, signer1), 0)
	assert.Error(s.T(), err)
	assert.Equal(s.T(), err.Error(), errFunds)

	//Test origin
	err = s.processor.ApplyTransaction(createTransaction(s.T(), obj1.Nonce(), obj2.address, 21, 5, signing.NewEdSigner()), 0)
	assert.Error(s.T(), err)
	assert.Equal(s.T(), err.Error(), errOrigin)
}

func (s *ProcessorStateSuite) TestTransactionProcessor_ApplyRewards() {
	s.processor.ApplyRewards(1, []types.Address{types.HexToAddress("aaa"),
		types.HexToAddress("bbb"),
		types.HexToAddress("ccc"),
		types.HexToAddress("ddd"),
		types.HexToAddress("bbb"),
		types.HexToAddress("aaa")},
		big.NewInt(1000),
	)

	assert.Equal(s.T(), s.processor.GetBalance(types.HexToAddress("aaa")), uint64(2000))
	assert.Equal(s.T(), s.processor.GetBalance(types.HexToAddress("bbb")), uint64(2000))
	assert.Equal(s.T(), s.processor.GetBalance(types.HexToAddress("ccc")), uint64(1000))
	assert.Equal(s.T(), s.processor.GetBalance(types.HexToAddress("ddd")), uint64(1000))
}

func (s *ProcessorStateSuite) TestTransactionProcessor_ApplyTransaction_OrderByNonce() {
	signerBuf := []byte("22222222222222222222222222222222")
	signerBuf = append(signerBuf, []byte{
		94, 33, 44, 9, 128, 228, 179, 159, 192, 151, 33, 19, 74, 160, 33, 9,
		55, 78, 223, 210, 96, 192, 211, 208, 60, 181, 1, 200, 214, 84, 87, 169,
	}...)
	signer, err := signing.NewEdSignerFromBuffer(signerBuf)
	assert.NoError(s.T(), err)
	obj1 := createAccount(s.processor, SignerToAddr(signer), 25, 0)
	obj2 := createAccount(s.processor, toAddr([]byte{0x01, 02}), 1, 10)
	obj3 := createAccount(s.processor, toAddr([]byte{0x02}), 44, 0)
	s.processor.Commit()

	transactions := []*types.Transaction{
		createTransaction(s.T(), obj1.Nonce()+3, obj3.address, 1, 5, signer),
		createTransaction(s.T(), obj1.Nonce()+2, obj3.address, 1, 5, signer),
		createTransaction(s.T(), obj1.Nonce()+1, obj3.address, 1, 5, signer),
		createTransaction(s.T(), obj1.Nonce(), obj2.address, 1, 5, signer),
	}

	s.processor.ApplyTransactions(1, transactions)
	//assert.Error(s.T(), err)

	got := string(s.processor.Dump())

	assert.Equal(s.T(), uint64(1), s.processor.GetBalance(obj1.address))
	assert.Equal(s.T(), uint64(2), s.processor.GetBalance(obj2.address))

	want := `{
	"root": "0fb9e074115e49b9a1d33949de2578459c158d8885ca10ad9edcd5d3a84fd67c",
	"accounts": {
		"0000000000000000000000000000000000000002": {
			"balance": "47",
			"nonce": 0
		},
		"0000000000000000000000000000000000000102": {
			"balance": "2",
			"nonce": 10
		},
		"4aa02109374edfd260c0d3d03cb501c8d65457a9": {
			"balance": "1",
			"nonce": 4
		}
	}
}`
	if got != want {
		s.T().Errorf("dump mismatch:\ngot: %s\nwant: %s\n", got, want)
	}
}

func (s *ProcessorStateSuite) TestTransactionProcessor_Reset() {
	lg := log.New("proc_logger", "", "")
	txDb := database.NewMemDatabase()
	db := database.NewMemDatabase()
	processor := NewTransactionProcessor(db, txDb, s.projector, NewTxMemPool(), lg)

	signer1Buf := []byte("22222222222222222222222222222222")
	signer1Buf = append(signer1Buf, []byte{
		94, 33, 44, 9, 128, 228, 179, 159, 192, 151, 33, 19, 74, 160, 33, 9,
		55, 78, 223, 210, 96, 192, 211, 208, 60, 181, 1, 200, 214, 84, 87, 169,
	}...)
	signer2Buf := []byte("33333333333333333333333333333333")
	signer2Buf = append(signer2Buf, []byte{
		23, 203, 121, 251, 43, 65, 32, 242, 177, 236, 101, 228, 25, 141, 110, 8,
		178, 142, 129, 63, 235, 1, 228, 164, 0, 131, 155, 133, 225, 128, 128, 206,
	}...)

	signer1, err := signing.NewEdSignerFromBuffer(signer1Buf)
	assert.NoError(s.T(), err)
	signer2, err := signing.NewEdSignerFromBuffer(signer2Buf)
	assert.NoError(s.T(), err)
	obj1 := createAccount(processor, SignerToAddr(signer1), 21, 0)
	obj2 := createAccount(processor, SignerToAddr(signer2), 41, 10)
	createAccount(processor, toAddr([]byte{0x02}), 44, 0)
	processor.Commit()

	transactions := []*types.Transaction{
		createTransaction(s.T(), obj1.Nonce(), obj2.address, 1, 5, signer1),
		//createTransaction(obj2.Nonce(),obj2.address, obj1.address, 1),
	}

	failed, err := processor.ApplyTransactions(1, transactions)
	assert.NoError(s.T(), err)
	assert.True(s.T(), failed == 0)

	transactions = []*types.Transaction{
		createTransaction(s.T(), obj1.Nonce(), obj2.address, 1, 5, signer1),
		createTransaction(s.T(), obj2.Nonce(), obj1.address, 10, 5, signer2),
	}

	failed, err = processor.ApplyTransactions(2, transactions)
	assert.True(s.T(), failed == 0)
	assert.NoError(s.T(), err)

	got := string(processor.Dump())

	want := `{
	"root": "4b7174d31e60ef1ed970137079e2b8044d9c381422dbcbe16e561d8a51a9f651",
	"accounts": {
		"0000000000000000000000000000000000000002": {
			"balance": "44",
			"nonce": 0
		},
		"198d6e08b28e813feb01e4a400839b85e18080ce": {
			"balance": "28",
			"nonce": 11
		},
		"4aa02109374edfd260c0d3d03cb501c8d65457a9": {
			"balance": "19",
			"nonce": 2
		}
	}
}`
	if got != want {
		s.T().Errorf("dump mismatch:\ngot: %s\nwant: %s\n", got, want)
	}

	err = processor.LoadState(1)
	assert.NoError(s.T(), err)

	got = string(processor.Dump())

	assert.Equal(s.T(), uint64(15), processor.GetBalance(obj1.address))

	want = `{
	"root": "9273645f6b9a62f32500021f5e0a89d3eb6ffd36b1b9f9f82fcaad4555951e97",
	"accounts": {
		"0000000000000000000000000000000000000002": {
			"balance": "44",
			"nonce": 0
		},
		"198d6e08b28e813feb01e4a400839b85e18080ce": {
			"balance": "42",
			"nonce": 10
		},
		"4aa02109374edfd260c0d3d03cb501c8d65457a9": {
			"balance": "15",
			"nonce": 1
		}
	}
}`
	if got != want {
		s.T().Errorf("dump mismatch:\ngot: %s\nwant: %s\n", got, want)
	}
}

func (s *ProcessorStateSuite) TestTransactionProcessor_Multilayer() {
	testCycles := 100
	maxTransactions := 20
	minTransactions := 1

	lg := log.New("proc_logger", "", "")
	txDb := database.NewMemDatabase()
	db := database.NewMemDatabase()
	processor := NewTransactionProcessor(db, txDb, s.projector, NewTxMemPool(), lg)

	revertToLayer := rand.Intn(testCycles)
	revertAfterLayer := rand.Intn(testCycles - revertToLayer) //rand.Intn(min(testCycles - revertToLayer,maxPas.processors))
	log.Info("starting test: revert on layer %v, after %v layers received since that layer ", revertToLayer, revertAfterLayer)

	signers := []*signing.EdSigner{
		signing.NewEdSigner(),
		signing.NewEdSigner(),
		signing.NewEdSigner(),
	}
	accounts := []*Object{
		createAccount(processor, toAddr(signers[0].PublicKey().Bytes()), 5218762487624, 0),
		createAccount(processor, toAddr(signers[1].PublicKey().Bytes()), 341578872634786, 10),
		createAccount(processor, toAddr(signers[2].PublicKey().Bytes()), 1044987234, 0),
	}

	processor.Commit()

	written := db.Len()

	var want string
	for i := 0; i < testCycles; i++ {
		numOfTransactions := rand.Intn(maxTransactions-minTransactions) + minTransactions
		trns := []*types.Transaction{}
		nonceTrack := make(map[*Object]int)
		for j := 0; j < numOfTransactions; j++ {

			src := int(rand.Uint32() % (uint32(len(accounts) - 1)))
			srcAccount := accounts[src]
			dstAccount := accounts[int(rand.Uint32()%(uint32(len(accounts)-1)))]

			if _, ok := nonceTrack[srcAccount]; !ok {
				nonceTrack[srcAccount] = 0
			} else {
				nonceTrack[srcAccount]++
			}

			for dstAccount == srcAccount {
				dstAccount = accounts[int(rand.Uint32()%(uint32(len(accounts)-1)))]
			}
			t := createTransaction(s.T(), processor.GetNonce(srcAccount.address)+uint64(nonceTrack[srcAccount]), dstAccount.address, (rand.Uint64()%srcAccount.Balance().Uint64())/100, 5, signers[src])
			trns = append(trns, t)

			log.Info("transaction %v nonce %v amount %v", t.Origin().Hex(), t.AccountNonce, t.Amount)
		}
		failed, err := processor.ApplyTransactions(types.LayerID(i), trns)
		assert.NoError(s.T(), err)
		assert.True(s.T(), failed == 0)

		if i == revertToLayer {
			want = string(processor.Dump())
			log.Info("wanted state: %v", want)
		}

		if i == revertToLayer+revertAfterLayer {
			err = processor.LoadState(types.LayerID(revertToLayer))
			assert.NoError(s.T(), err)
			got := string(processor.Dump())

			if got != want {
				s.T().Errorf("dump mismatch:\ngot: %s\nwant: %s\n", got, want)
			}
		}

	}

	writtenMore := db.Len()

	assert.True(s.T(), writtenMore > written)
}

func newTx(t *testing.T, nonce, totalAmount uint64, signer *signing.EdSigner) *types.Transaction {
	feeAmount := uint64(1)
	rec := types.Address{byte(rand.Int()), byte(rand.Int()), byte(rand.Int()), byte(rand.Int())}
	return createTransaction(t, nonce, rec, totalAmount-feeAmount, feeAmount, signer)
}

func (s *ProcessorStateSuite) TestTransactionProcessor_ValidateNonceAndBalance() {
	r := require.New(s.T())
	signer := signing.NewEdSigner()
	origin := types.BytesToAddress(signer.PublicKey().Bytes())
	s.processor.SetBalance(origin, big.NewInt(100))
	s.processor.SetNonce(origin, 5)
	s.projector.balanceDiff = 10
	s.projector.nonceDiff = 2

	err := s.processor.ValidateNonceAndBalance(newTx(s.T(), 7, 10, signer))
	r.NoError(err)
}

func (s *ProcessorStateSuite) TestTransactionProcessor_ValidateNonceAndBalance_WrongNonce() {
	r := require.New(s.T())
	signer := signing.NewEdSigner()
	origin := types.BytesToAddress(signer.PublicKey().Bytes())
	s.processor.SetBalance(origin, big.NewInt(100))
	s.processor.SetNonce(origin, 5)
	s.projector.balanceDiff = 10
	s.projector.nonceDiff = 2

	err := s.processor.ValidateNonceAndBalance(newTx(s.T(), 8, 10, signer))
	r.EqualError(err, "incorrect account nonce! Expected: 7, Actual: 8")
}

func (s *ProcessorStateSuite) TestTransactionProcessor_ValidateNonceAndBalance_InsufficientBalance() {
	r := require.New(s.T())
	signer := signing.NewEdSigner()
	origin := types.BytesToAddress(signer.PublicKey().Bytes())
	s.processor.SetBalance(origin, big.NewInt(100))
	s.processor.SetNonce(origin, 5)
	s.projector.balanceDiff = 10
	s.projector.nonceDiff = 2

	err := s.processor.ValidateNonceAndBalance(newTx(s.T(), 7, 95, signer))
	r.EqualError(err, "insufficient balance! Available: 90, Attempting to spend: 94[amount]+1[fee]=95")
}

func TestTransactionProcessor_ApplyTransactionTestSuite(t *testing.T) {
	suite.Run(t, new(ProcessorStateSuite))
}

func createXdrSignedTransaction(t *testing.T, key ed25519.PrivateKey) *types.Transaction {
	r := require.New(t)
	signer, err := signing.NewEdSignerFromBuffer(key)
	r.NoError(err)
	tx, err := types.NewSignedTx(1111, toAddr([]byte{0xde}), 123, 11, 456, signer)
	r.NoError(err)
	return tx
}

func TestValidateTxSignature(t *testing.T) {
	db := database.NewMemDatabase()
	lg := log.New("proc_logger", "", "")
	proc := NewTransactionProcessor(db, appliedTxsMock{}, &ProjectorMock{}, NewTxMemPool(), lg)

	// positive flow
	pub, pri, _ := ed25519.GenerateKey(crand.Reader)
	createAccount(proc, PublicKeyToAccountAddress(pub), 123, 321)
	tx := createXdrSignedTransaction(t, pri)

	assert.Equal(t, PublicKeyToAccountAddress(pub), tx.Origin())
	assert.True(t, proc.AddressExists(tx.Origin()))

	// negative flow
	pub, pri, _ = ed25519.GenerateKey(crand.Reader)
	tx = createXdrSignedTransaction(t, pri)

	assert.False(t, proc.AddressExists(tx.Origin()))
	assert.Equal(t, PublicKeyToAccountAddress(pub), tx.Origin())
}

func TestTransactionProcessor_GetStateRoot(t *testing.T) {
	r := require.New(t)

	db := database.NewMemDatabase()
	lg := log.New("proc_logger", "", "")
	proc := NewTransactionProcessor(db, appliedTxsMock{}, &ProjectorMock{}, NewTxMemPool(), lg)

	r.NotEqual(types.Hash32{}, proc.rootHash)

	expectedRoot := types.Hash32{1, 2, 3}
	r.NoError(proc.addState(expectedRoot, 1))

	actualRoot := proc.GetStateRoot()
	r.Equal(expectedRoot, actualRoot)
}

func TestTransactionProcessor_ApplyTransactions(t *testing.T) {
	lg := log.New("proc_logger", "", "")
	db := database.NewMemDatabase()
	projector := &ProjectorMock{}
	processor := NewTransactionProcessor(db, db, projector, NewTxMemPool(), lg)

	signerBuf := []byte("22222222222222222222222222222222")
	signerBuf = append(signerBuf, []byte{
		94, 33, 44, 9, 128, 228, 179, 159, 192, 151, 33, 19, 74, 160, 33, 9,
		55, 78, 223, 210, 96, 192, 211, 208, 60, 181, 1, 200, 214, 84, 87, 169,
	}...)
	signer, err := signing.NewEdSignerFromBuffer(signerBuf)
	assert.NoError(t, err)
	obj1 := createAccount(processor, SignerToAddr(signer), 21, 0)
	obj2 := createAccount(processor, toAddr([]byte{0x01, 02}), 1, 10)
	createAccount(processor, toAddr([]byte{0x02}), 44, 0)
	processor.Commit()

	transactions := []*types.Transaction{
		createTransaction(t, obj1.Nonce(), obj2.address, 1, 5, signer),
	}

	_, err = processor.ApplyTransactions(1, transactions)
	assert.NoError(t, err)

	_, err = processor.ApplyTransactions(2, []*types.Transaction{})
	assert.NoError(t, err)

	_, err = processor.ApplyTransactions(3, []*types.Transaction{})
	assert.NoError(t, err)

	_, err = processor.GetLayerStateRoot(3)
	assert.NoError(t, err)

}
