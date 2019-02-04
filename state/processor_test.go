package state

import (
	"github.com/seehuhn/mt19937"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"math/big"
	"math/rand"
	"testing"
)

type ProcessorStateSuite struct {
	suite.Suite
	db    *database.MemDatabase
	state *StateDB
	processor *TransactionProcessor
}

func (s *ProcessorStateSuite) SetupTest() {
	rng := rand.New(mt19937.New())

	s.db = database.NewMemDatabase()
	s.state, _ = New(common.Hash{}, NewDatabase(s.db))

	s.processor = NewTransactionProcessor(rng, s.state)
}

func createAccount(state *StateDB, addr []byte, balance int64, nonce uint64) *StateObj{
	addr1 := toAddr(addr)
	obj1 := state.GetOrNewStateObj(addr1)
	obj1.AddBalance(big.NewInt(balance))
	obj1.SetNonce(nonce)
	state.updateStateObj(obj1)
	return  obj1
}

func createTransaction(nonce uint64,
	origin address.Address, destination address.Address, amount int64) *Transaction{
	return &Transaction{
		AccountNonce: nonce,
		Origin: origin,
		Recipient:&destination,
		Amount: big.NewInt(amount),
		GasLimit:10,
		Price:big.NewInt(1),
		hash:nil,
		Payload:nil,
	}
}

func (s *ProcessorStateSuite) TestTransactionProcessor_ApplyTransaction() {
	//test happy flow
	//test happy flow with underlying structures
	//test insufficient funds
	//test wrong nonce
	//test account doesn't exist
	obj1 := createAccount(s.state, []byte{0x01}, 21, 0)
	obj2 := createAccount(s.state, []byte{0x01,02}, 1, 10)
	createAccount(s.state, []byte{0x02}, 44, 0)
	s.state.Commit(false)

	transactions := Transactions{
		createTransaction(obj1.Nonce(),obj1.address, obj2.address, 1),
	}


	failed, err := s.processor.ApplyTransactions(1, transactions)
	assert.NoError(s.T(), err)
	assert.True(s.T(), failed == 0)

	got := string(s.state.Dump())

	assert.Equal(s.T(), big.NewInt(20),s.state.GetBalance(obj1.address))
	assert.Equal(s.T(), uint64(1),s.state.GetNonce(obj1.address))

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
}

func (s *ProcessorStateSuite) TestTransactionProcessor_ApplyTransaction_DoubleTrans() {
	//test happy flow
	//test happy flow with underlying structures
	//test insufficient funds
	//test wrong nonce
	//test account doesn't exist
	obj1 := createAccount(s.state, []byte{0x01}, 21, 0)
	obj2 := createAccount(s.state, []byte{0x01,02}, 1, 10)
	createAccount(s.state, []byte{0x02}, 44, 0)
	s.state.Commit(false)

	transactions := Transactions{
		createTransaction(obj1.Nonce(),obj1.address, obj2.address, 1),
		createTransaction(obj1.Nonce(),obj1.address, obj2.address, 1),
		createTransaction(obj1.Nonce(),obj1.address, obj2.address, 1),
		createTransaction(obj1.Nonce(),obj1.address, obj2.address, 1),
	}


	failed, err := s.processor.ApplyTransactions(1, transactions)
	assert.NoError(s.T(), err)
	assert.True(s.T(), failed == 0)

	got := string(s.state.Dump())

	assert.Equal(s.T(), big.NewInt(20),s.state.GetBalance(obj1.address))

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
}

func (s *ProcessorStateSuite) TestTransactionProcessor_ApplyTransaction_Errors() {
	obj1 := createAccount(s.state, []byte{0x01}, 21, 0)
	obj2 := createAccount(s.state, []byte{0x01, 02}, 1, 10)
	createAccount(s.state, []byte{0x02}, 44, 0)
	s.state.Commit(false)

	transactions := Transactions{
		createTransaction(obj1.Nonce(),obj1.address, obj2.address, 1),
	}


	failed,err := s.processor.ApplyTransactions(1, transactions)
	assert.NoError(s.T(), err)
	assert.True(s.T(), failed == 0)

	err = s.processor.ApplyTransaction(createTransaction(0,obj1.address, obj2.address, 1))
	assert.Error(s.T(), err)
	assert.Equal(s.T(), err.Error(), ErrNonce)


	err = s.processor.ApplyTransaction(createTransaction(obj1.Nonce(),obj1.address, obj2.address, 21))
	assert.Error(s.T(), err)
	assert.Equal(s.T(), err.Error(), ErrFunds)

	addr := toAddr([]byte{0x01, 0x01})

	//Test origin
	err = s.processor.ApplyTransaction(createTransaction(obj1.Nonce(),addr, obj2.address, 21))
	assert.Error(s.T(), err)
	assert.Equal(s.T(), err.Error(), ErrOrigin)
}


func (s *ProcessorStateSuite) TestTransactionProcessor_ApplyTransaction_OrderByNonce() {
	obj1 := createAccount(s.state,[]byte{0x01}, 5, 0)
	obj2 := createAccount(s.state,[]byte{0x01, 02}, 1, 10)
	obj3 := createAccount(s.state,[]byte{0x02}, 44, 0)
	s.state.Commit(false)

	transactions := Transactions{
		createTransaction(obj1.Nonce() +3, obj1.address, obj3.address, 1),
		createTransaction(obj1.Nonce() +2, obj1.address, obj3.address, 1),
		createTransaction(obj1.Nonce() +1, obj1.address, obj3.address, 1),
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 1),
	}

	s.processor.ApplyTransactions(1, transactions)
	//assert.Error(s.T(), err)

	got := string(s.state.Dump())

	assert.Equal(s.T(), big.NewInt(1),s.state.GetBalance(obj1.address))
	assert.Equal(s.T(), big.NewInt(2),s.state.GetBalance(obj2.address))

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
	obj1 := createAccount(s.state, []byte{0x01}, 21, 0)
	obj2 := createAccount(s.state, []byte{0x01,02}, 41, 10)
	createAccount(s.state, []byte{0x02}, 44, 0)
	s.state.Commit(false)

	transactions := Transactions{
		createTransaction(obj1.Nonce(),obj1.address, obj2.address, 1),
		//createTransaction(obj2.Nonce(),obj2.address, obj1.address, 1),
	}


	failed, err := s.processor.ApplyTransactions(1, transactions)
	assert.NoError(s.T(), err)
	assert.True(s.T(), failed == 0)

	transactions = Transactions{
		createTransaction(obj1.Nonce(),obj1.address, obj2.address, 1),
		createTransaction(obj2.Nonce(),obj2.address, obj1.address, 10),
	}


	failed, err = s.processor.ApplyTransactions(2, transactions)
	assert.True(s.T(), failed == 0)
	assert.NoError(s.T(), err)

	got := string(s.processor.globalState.Dump())

	want := `{
	"root": "ad5e89c8f94168027dc1df5da112c76c330ffcbc7df3e73c29f1728e488b77b2",
	"accounts": {
		"0000000000000000000000000000000000000001": {
			"balance": "29",
			"nonce": 2
		},
		"0000000000000000000000000000000000000002": {
			"balance": "44",
			"nonce": 0
		},
		"0000000000000000000000000000000000000102": {
			"balance": "33",
			"nonce": 11
		}
	}
}`
	if got != want {
		s.T().Errorf("dump mismatch:\ngot: %s\nwant: %s\n", got, want)
	}

	s.processor.Reset(1)

	got = string(s.processor.globalState.Dump())

	assert.Equal(s.T(), big.NewInt(20),s.processor.globalState.GetBalance(obj1.address))

	want = `{
	"root": "0f8918fdba9e0a1542ac89ff9a60c657ff4c5800a700266ecfdcfc2d4e1fad3e",
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
	revertAfterLayer := rand.Intn(testCycles - revertToLayer)//rand.Intn(min(testCycles - revertToLayer,maxPastStates))
	log.Info("starting test: revert on layer %v, after %v layers received since that layer ", revertToLayer, revertAfterLayer)

	obj1 := createAccount(s.state, []byte{0x01}, 5218762487624, 0)
	obj2 := createAccount(s.state, []byte{0x01, 02}, 341578872634786, 10)
	obj3 := createAccount(s.state, []byte{0x02}, 1044987234, 0)

	s.state.Commit(false)

	written := s.db.Len()
	accounts := []*StateObj{obj1, obj2, obj3}

	var want string
	for i := 0; i < testCycles; i++ {
		numOfTransactions := rand.Intn(maxTransactions - minTransactions) + minTransactions
		trns := Transactions{}
		nonceTrack := make(map[*StateObj]int)
		for j := 0; j < numOfTransactions; j++ {


			srcAccount := accounts[int(rand.Uint32() % (uint32(len(accounts) -1) ))]
			dstAccount := accounts[int(rand.Uint32() % (uint32(len(accounts) -1) ))]

			if _,ok := nonceTrack[srcAccount]; !ok {
				nonceTrack[srcAccount] =0
			} else {
				nonceTrack[srcAccount]++
			}

			for dstAccount == srcAccount {
				dstAccount = accounts[int(rand.Uint32() % (uint32(len(accounts) -1) ))]
			}
			t := createTransaction(s.processor.globalState.GetNonce(srcAccount.address)+ uint64(nonceTrack[srcAccount]),
				srcAccount.address, dstAccount.address, int64(rand.Uint64() % srcAccount.Balance().Uint64() )/100)
			trns = append(trns,  t)

			log.Info("transaction %v nonce %v amount %v", t.Origin.Hex(), t.AccountNonce, t.Amount)
		}
		failed, err := s.processor.ApplyTransactions(LayerID(i), trns)
		assert.NoError(s.T(),err)
		assert.True(s.T(), failed == 0)

		if i == revertToLayer {
			want = string(s.processor.globalState.Dump())
			log.Info("wanted state: %v", want)
		}

		if i == revertToLayer + revertAfterLayer {
			s.processor.Reset(LayerID(revertToLayer))
			got := string(s.processor.globalState.Dump())

			if got != want {
				s.T().Errorf("dump mismatch:\ngot: %s\nwant: %s\n", got, want)
			}
		}

	}

	writtenMore := s.db.Len()

	assert.True(s.T(), writtenMore > written)
}


func TestTransactionProcessor_ApplyTransactionTestSuite(t *testing.T){
	suite.Run(t, new(ProcessorStateSuite))
}

func TestTransactionProcessor_randomSort(t *testing.T) {
	rng := rand.New(mt19937.New())
	rng.Seed(1)
	db := database.NewMemDatabase()
	state, _ := New(common.Hash{}, NewDatabase(db))

	processor := NewTransactionProcessor(rng, state)

	obj1 := createAccount(state,[]byte{0x01}, 2, 0)
	obj2 := createAccount(state,[]byte{0x01, 02}, 1, 10)

	transactions := Transactions{
		createTransaction(obj1.Nonce(),obj1.address, obj2.address, 1),
		createTransaction(obj1.Nonce(),obj1.address, obj2.address, 2),
		createTransaction(obj1.Nonce(),obj2.address, obj1.address, 3),
		createTransaction(obj1.Nonce(),obj1.address, obj2.address, 4),
		createTransaction(obj1.Nonce(),obj2.address, obj1.address, 5),
	}

	expected := Transactions{
		transactions[4],
		transactions[3],
		transactions[1],
		transactions[0],
		transactions[2],
	}

	trans := processor.randomSort(transactions)

	assert.Equal(t, expected, trans)
}
