package state

import (
	crand "crypto/rand"
	"github.com/seehuhn/mt19937"
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
	state     *StateDB
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

func (s *ProcessorStateSuite) SetupTest() {
	rng := rand.New(mt19937.New())
	lg := log.New("proc_logger", "", "")
	s.db = database.NewMemDatabase()
	s.state, _ = New(types.Hash32{}, NewDatabase(s.db))
	s.projector = &ProjectorMock{}

	s.processor = NewTransactionProcessor(rng, s.state, s.projector, GasConfig{big.NewInt(5)}, lg)
}

func createAccount(state *StateDB, addr types.Address, balance int64, nonce uint64) *StateObj {
	obj1 := state.GetOrNewStateObj(addr)
	obj1.AddBalance(big.NewInt(balance))
	obj1.SetNonce(nonce)
	state.updateStateObj(obj1)
	return obj1
}

func createTransaction(nonce uint64, origin types.Address, destination types.Address, amount uint64) *types.Transaction {
	return types.NewTxWithOrigin(nonce, origin, destination, amount, 100, 1)
}

func (s *ProcessorStateSuite) TestTransactionProcessor_ApplyTransaction() {
	//test happy flow
	//test happy flow with underlying structures
	//test insufficient funds
	//test wrong nonce
	//test account doesn't exist
	obj1 := createAccount(s.state, toAddr([]byte{0x01}), 21, 0)
	obj2 := createAccount(s.state, toAddr([]byte{0x01, 02}), 1, 10)
	createAccount(s.state, toAddr([]byte{0x02}), 44, 0)
	s.state.Commit(false)

	transactions := []*types.Transaction{
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 1),
	}

	failed, err := s.processor.ApplyTransactions(1, transactions)
	assert.NoError(s.T(), err)
	assert.True(s.T(), failed == 0)

	got := string(s.state.Dump())

	assert.Equal(s.T(), uint64(15), s.state.GetBalance(obj1.address))
	assert.Equal(s.T(), uint64(1), s.state.GetNonce(obj1.address))

	want := `{
	"root": "8e80968ce4ed49cb0caf0036691d531fc0db419b7ea3018fe0a1263de9bd886c",
	"accounts": {
		"0000000000000000000000000000000000000001": {
			"balance": "15",
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
}

/*func (s *ProcessorStateSuite) TestTransactionProcessor_ApplyTransaction_DoubleTrans() {
	//test happy flow
	//test happy flow with underlying structures
	//test insufficient funds
	//test wrong nonce
	//test account doesn't exist
	obj1 := createAccount(s.state, []byte{0x01}, 21, 0)
	obj2 := createAccount(s.state, []byte{0x01, 02}, 1, 10)
	createAccount(s.state, []byte{0x02}, 44, 0)
	s.state.Commit(false)

	transactions := Transactions{
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 1),
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 1),
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 1),
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 1),
	}

	failed, err := s.processor.ApplyTransactions(1, transactions)
	assert.NoError(s.T(), err)
	assert.True(s.T(), failed == 0)

	got := string(s.state.Dump())

	assert.Equal(s.T(), big.NewInt(20), s.state.GetBalance(obj1.address))

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
	obj1 := createAccount(s.state, toAddr([]byte{0x01}), 21, 0)
	obj2 := createAccount(s.state, toAddr([]byte{0x01, 02}), 1, 10)
	createAccount(s.state, toAddr([]byte{0x02}), 44, 0)
	s.state.Commit(false)

	transactions := []*types.Transaction{
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 1),
	}

	failed, err := s.processor.ApplyTransactions(1, transactions)
	assert.NoError(s.T(), err)
	assert.True(s.T(), failed == 0)

	err = s.processor.ApplyTransaction(createTransaction(0, obj1.address, obj2.address, 1))
	assert.Error(s.T(), err)
	assert.Equal(s.T(), err.Error(), ErrNonce)

	err = s.processor.ApplyTransaction(createTransaction(obj1.Nonce(), obj1.address, obj2.address, 21))
	assert.Error(s.T(), err)
	assert.Equal(s.T(), err.Error(), ErrFunds)

	addr := toAddr([]byte{0x01, 0x01})

	//Test origin
	err = s.processor.ApplyTransaction(createTransaction(obj1.Nonce(), addr, obj2.address, 21))
	assert.Error(s.T(), err)
	assert.Equal(s.T(), err.Error(), ErrOrigin)
}

func (s *ProcessorStateSuite) TestTransactionProcessor_ApplyRewards() {
	s.processor.ApplyRewards(1, []types.Address{types.HexToAddress("aaa"),
		types.HexToAddress("bbb"),
		types.HexToAddress("ccc"),
		types.HexToAddress("ddd"),
		types.HexToAddress("bbb"),
		types.HexToAddress("aaa")},
		map[types.Address]int{types.HexToAddress("aaa"): 1, types.HexToAddress("bbb"): 2},
		big.NewInt(1000), big.NewInt(300))

	assert.Equal(s.T(), s.state.GetBalance(types.HexToAddress("aaa")), uint64(1300))
	assert.Equal(s.T(), s.state.GetBalance(types.HexToAddress("bbb")), uint64(600))
	assert.Equal(s.T(), s.state.GetBalance(types.HexToAddress("ccc")), uint64(1000))
	assert.Equal(s.T(), s.state.GetBalance(types.HexToAddress("ddd")), uint64(1000))
}

func (s *ProcessorStateSuite) TestTransactionProcessor_ApplyTransaction_OrderByNonce() {
	obj1 := createAccount(s.state, toAddr([]byte{0x01}), 25, 0)
	obj2 := createAccount(s.state, toAddr([]byte{0x01, 02}), 1, 10)
	obj3 := createAccount(s.state, toAddr([]byte{0x02}), 44, 0)
	s.state.Commit(false)

	transactions := []*types.Transaction{
		createTransaction(obj1.Nonce()+3, obj1.address, obj3.address, 1),
		createTransaction(obj1.Nonce()+2, obj1.address, obj3.address, 1),
		createTransaction(obj1.Nonce()+1, obj1.address, obj3.address, 1),
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 1),
	}

	s.processor.ApplyTransactions(1, transactions)
	//assert.Error(s.T(), err)

	got := string(s.state.Dump())

	assert.Equal(s.T(), uint64(1), s.state.GetBalance(obj1.address))
	assert.Equal(s.T(), uint64(2), s.state.GetBalance(obj2.address))

	want := `{
	"root": "e5212ec1f253fc4d7f77a591f66770ccae676ece70823287638ad7c5f988bced",
	"accounts": {
		"0000000000000000000000000000000000000001": {
			"balance": "1",
			"nonce": 4
		},
		"0000000000000000000000000000000000000002": {
			"balance": "47",
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
}

func (s *ProcessorStateSuite) TestTransactionProcessor_Reset() {
	obj1 := createAccount(s.state, toAddr([]byte{0x01}), 21, 0)
	obj2 := createAccount(s.state, toAddr([]byte{0x01, 02}), 41, 10)
	createAccount(s.state, toAddr([]byte{0x02}), 44, 0)
	s.state.Commit(false)

	transactions := []*types.Transaction{
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 1),
		//createTransaction(obj2.Nonce(),obj2.address, obj1.address, 1),
	}

	failed, err := s.processor.ApplyTransactions(1, transactions)
	assert.NoError(s.T(), err)
	assert.True(s.T(), failed == 0)

	transactions = []*types.Transaction{
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 1),
		createTransaction(obj2.Nonce(), obj2.address, obj1.address, 10),
	}

	failed, err = s.processor.ApplyTransactions(2, transactions)
	assert.True(s.T(), failed == 0)
	assert.NoError(s.T(), err)

	got := string(s.processor.globalState.Dump())

	want := `{
	"root": "abe8e506a6a6aeb5e0eb14e7733c85d60f619a93cabe8579fc16c3c106a0ffb1",
	"accounts": {
		"0000000000000000000000000000000000000001": {
			"balance": "19",
			"nonce": 2
		},
		"0000000000000000000000000000000000000002": {
			"balance": "44",
			"nonce": 0
		},
		"0000000000000000000000000000000000000102": {
			"balance": "28",
			"nonce": 11
		}
	}
}`
	if got != want {
		s.T().Errorf("dump mismatch:\ngot: %s\nwant: %s\n", got, want)
	}

	s.processor.Reset(1)

	got = string(s.processor.globalState.Dump())

	assert.Equal(s.T(), uint64(15), s.processor.globalState.GetBalance(obj1.address))

	want = `{
	"root": "351199c464a9be1b7c78aeecb6f83f04e38316fa98d7d45a2d9fc475d4c3b857",
	"accounts": {
		"0000000000000000000000000000000000000001": {
			"balance": "15",
			"nonce": 1
		},
		"0000000000000000000000000000000000000002": {
			"balance": "44",
			"nonce": 0
		},
		"0000000000000000000000000000000000000102": {
			"balance": "42",
			"nonce": 10
		}
	}
}`
	if got != want {
		s.T().Errorf("dump mismatch:\ngot: %s\nwant: %s\n", got, want)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *ProcessorStateSuite) TestTransactionProcessor_Multilayer() {
	testCycles := 100
	maxTransactions := 20
	minTransactions := 1

	revertToLayer := rand.Intn(testCycles)
	revertAfterLayer := rand.Intn(testCycles - revertToLayer) //rand.Intn(min(testCycles - revertToLayer,maxPastStates))
	log.Info("starting test: revert on layer %v, after %v layers received since that layer ", revertToLayer, revertAfterLayer)

	obj1 := createAccount(s.state, toAddr([]byte{0x01}), 5218762487624, 0)
	obj2 := createAccount(s.state, toAddr([]byte{0x01, 02}), 341578872634786, 10)
	obj3 := createAccount(s.state, toAddr([]byte{0x02}), 1044987234, 0)

	s.state.Commit(false)

	written := s.db.Len()
	accounts := []*StateObj{obj1, obj2, obj3}

	var want string
	for i := 0; i < testCycles; i++ {
		numOfTransactions := rand.Intn(maxTransactions-minTransactions) + minTransactions
		trns := []*types.Transaction{}
		nonceTrack := make(map[*StateObj]int)
		for j := 0; j < numOfTransactions; j++ {

			srcAccount := accounts[int(rand.Uint32()%(uint32(len(accounts)-1)))]
			dstAccount := accounts[int(rand.Uint32()%(uint32(len(accounts)-1)))]

			if _, ok := nonceTrack[srcAccount]; !ok {
				nonceTrack[srcAccount] = 0
			} else {
				nonceTrack[srcAccount]++
			}

			for dstAccount == srcAccount {
				dstAccount = accounts[int(rand.Uint32()%(uint32(len(accounts)-1)))]
			}
			t := createTransaction(s.processor.globalState.GetNonce(srcAccount.address)+uint64(nonceTrack[srcAccount]),
				srcAccount.address, dstAccount.address, (rand.Uint64()%srcAccount.Balance().Uint64())/100)
			trns = append(trns, t)

			log.Info("transaction %v nonce %v amount %v", t.Origin().Hex(), t.AccountNonce, t.Amount)
		}
		failed, err := s.processor.ApplyTransactions(types.LayerID(i), trns)
		assert.NoError(s.T(), err)
		assert.True(s.T(), failed == 0)

		if i == revertToLayer {
			want = string(s.processor.globalState.Dump())
			log.Info("wanted state: %v", want)
		}

		if i == revertToLayer+revertAfterLayer {
			s.processor.Reset(types.LayerID(revertToLayer))
			got := string(s.processor.globalState.Dump())

			if got != want {
				s.T().Errorf("dump mismatch:\ngot: %s\nwant: %s\n", got, want)
			}
		}

	}

	writtenMore := s.db.Len()

	assert.True(s.T(), writtenMore > written)
}

func newTx(origin types.Address, nonce, totalAmount uint64) *types.Transaction {
	feeAmount := uint64(1)
	return types.NewTxWithOrigin(nonce, origin, types.Address{}, totalAmount-feeAmount, 3, feeAmount)
}

func (s *ProcessorStateSuite) TestTransactionProcessor_ValidateNonceAndBalance() {
	r := require.New(s.T())
	origin := types.BytesToAddress([]byte("abc"))
	s.processor.globalState.SetBalance(origin, big.NewInt(100))
	s.processor.globalState.SetNonce(origin, 5)
	s.projector.balanceDiff = 10
	s.projector.nonceDiff = 2

	err := s.processor.ValidateNonceAndBalance(newTx(origin, 7, 10))
	r.NoError(err)
}

func (s *ProcessorStateSuite) TestTransactionProcessor_ValidateNonceAndBalance_WrongNonce() {
	r := require.New(s.T())
	origin := types.BytesToAddress([]byte("abc"))
	s.processor.globalState.SetBalance(origin, big.NewInt(100))
	s.processor.globalState.SetNonce(origin, 5)
	s.projector.balanceDiff = 10
	s.projector.nonceDiff = 2

	err := s.processor.ValidateNonceAndBalance(newTx(origin, 8, 10))
	r.EqualError(err, "incorrect account nonce! Expected: 7, Actual: 8")
}

func (s *ProcessorStateSuite) TestTransactionProcessor_ValidateNonceAndBalance_InsufficientBalance() {
	r := require.New(s.T())
	origin := types.BytesToAddress([]byte("abc"))
	s.processor.globalState.SetBalance(origin, big.NewInt(100))
	s.processor.globalState.SetNonce(origin, 5)
	s.projector.balanceDiff = 10
	s.projector.nonceDiff = 2

	err := s.processor.ValidateNonceAndBalance(newTx(origin, 7, 95))
	r.EqualError(err, "insufficient balance! Available: 90, Attempting to spend: 94[amount]+1[fee]=95")
}

func TestTransactionProcessor_ApplyTransactionTestSuite(t *testing.T) {
	suite.Run(t, new(ProcessorStateSuite))
}

func TestTransactionProcessor_randomSort(t *testing.T) {
	rng := rand.New(mt19937.New())
	rng.Seed(1)
	db := database.NewMemDatabase()
	state, _ := New(types.Hash32{}, NewDatabase(db))
	lg := log.New("proc_logger", "", "")
	processor := NewTransactionProcessor(rng, state, &ProjectorMock{}, GasConfig{big.NewInt(5)}, lg)

	obj1 := createAccount(state, toAddr([]byte{0x01}), 2, 0)
	obj2 := createAccount(state, toAddr([]byte{0x01, 02}), 1, 10)

	transactions := []*types.Transaction{
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 1),
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 2),
		createTransaction(obj1.Nonce(), obj2.address, obj1.address, 3),
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 4),
		createTransaction(obj1.Nonce(), obj2.address, obj1.address, 5),
	}

	expected := []*types.Transaction{
		transactions[4],
		transactions[3],
		transactions[1],
		transactions[0],
		transactions[2],
	}

	trans := processor.randomSort(transactions)

	assert.Equal(t, expected, trans)
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
	rng := rand.New(mt19937.New())
	rng.Seed(1)
	db := database.NewMemDatabase()
	state, _ := New(types.Hash32{}, NewDatabase(db))
	lg := log.New("proc_logger", "", "")
	proc := NewTransactionProcessor(rng, state, &ProjectorMock{}, GasConfig{big.NewInt(5)}, lg)

	// positive flow
	pub, pri, _ := ed25519.GenerateKey(crand.Reader)
	createAccount(state, PublicKeyToAccountAddress(pub), 123, 321)
	tx := createXdrSignedTransaction(t, pri)

	assert.Equal(t, PublicKeyToAccountAddress(pub), tx.Origin())
	assert.True(t, proc.AddressExists(tx.Origin()))

	// negative flow
	pub, pri, _ = ed25519.GenerateKey(crand.Reader)
	tx = createXdrSignedTransaction(t, pri)

	assert.False(t, proc.AddressExists(tx.Origin()))
	assert.Equal(t, PublicKeyToAccountAddress(pub), tx.Origin())
}
