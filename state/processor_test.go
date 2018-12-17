package state

import (
	"github.com/seehuhn/mt19937"
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
	origin common.Address, destination common.Address, amount int64) *Transaction{
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


	err := s.processor.ApplyTransactions(transactions)
	assert.NoError(s.T(), err)

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


	err := s.processor.ApplyTransactions(transactions)
	assert.NoError(s.T(), err)

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


	//Test Incorrect nonce
	transactions = Transactions{
		createTransaction(obj1.Nonce(),obj1.address, obj2.address, 1),
		createTransaction(obj1.Nonce(),obj1.address, obj2.address, 2),
	}

	for _, trns := range transactions {
		log.Info("trans hash %x", trns.Hash())
	}

	err = s.processor.ApplyTransactions(transactions)
	assert.Error(s.T(), err)
	assert.Equal(s.T(), err.Error(), ErrNonce)

	//Test Insufficient funds
	transactions = Transactions{
		createTransaction(obj1.Nonce() +1,obj1.address, obj2.address, 21),
	}

	for _, trns := range transactions {
		log.Info("trans hash %x", trns.Hash())
	}

	err = s.processor.ApplyTransactions(transactions)
	assert.Error(s.T(), err)
	assert.Equal(s.T(), err.Error(), ErrFunds)

	addr := toAddr([]byte{0x01, 0x01})

	//Test Insufficient funds
	transactions = Transactions{
		createTransaction(obj1.Nonce(),addr, obj2.address, 21),
	}

	for _, trns := range transactions {
		log.Info("trans hash %x", trns.Hash())
	}

	err = s.processor.ApplyTransactions(transactions)
	assert.Error(s.T(), err)
	assert.Equal(s.T(), err.Error(), ErrOrigin)
}

func (s *ProcessorStateSuite) TestTransactionProcessor_ApplyTransaction_Errors() {
	obj1 := createAccount(s.state, []byte{0x01}, 21, 0)
	obj2 := createAccount(s.state, []byte{0x01, 02}, 1, 10)
	createAccount(s.state, []byte{0x02}, 44, 0)
	s.state.Commit(false)

	transactions := Transactions{
		createTransaction(obj1.Nonce(),obj1.address, obj2.address, 1),
	}


	err := s.processor.ApplyTransactions(transactions)
	assert.NoError(s.T(), err)

	//Test Incorrect nonce
	transactions = Transactions{
		createTransaction(obj1.Nonce(),obj1.address, obj2.address, 1),
		createTransaction(obj1.Nonce(),obj1.address, obj2.address, 2),
	}

	for _, trns := range transactions {
		log.Info("trans hash %x", trns.Hash())
	}

	err = s.processor.ApplyTransactions(transactions)
	assert.Error(s.T(), err)
	assert.Equal(s.T(), err.Error(), ErrNonce)

	//Test Insufficient funds
	transactions = Transactions{
		createTransaction(obj1.Nonce() +1,obj1.address, obj2.address, 21),
	}

	for _, trns := range transactions {
		log.Info("trans hash %x", trns.Hash())
	}

	err = s.processor.ApplyTransactions(transactions)
	assert.Error(s.T(), err)
	assert.Equal(s.T(), err.Error(), ErrFunds)

	addr := toAddr([]byte{0x01, 0x01})

	//Test Insufficient funds
	transactions = Transactions{
		createTransaction(obj1.Nonce(),addr, obj2.address, 21),
	}

	for _, trns := range transactions {
		log.Info("trans hash %x", trns.Hash())
	}

	err = s.processor.ApplyTransactions(transactions)
	assert.Error(s.T(), err)
	assert.Equal(s.T(), err.Error(), ErrOrigin)
}


func (s *ProcessorStateSuite) TestTransactionProcessor_ApplyTransaction_OrderByNonce() {
	obj1 := createAccount(s.state,[]byte{0x01}, 2, 0)
	obj2 := createAccount(s.state,[]byte{0x01, 02}, 1, 10)
	obj3 := createAccount(s.state,[]byte{0x02}, 44, 0)
	s.state.Commit(false)

	transactions := Transactions{
		createTransaction(obj1.Nonce() +1, obj1.address, obj3.address, 1),
		createTransaction(obj1.Nonce(), obj1.address, obj2.address, 1),
	}

	err := s.processor.ApplyTransactions(transactions)
	assert.Error(s.T(), err)

	got := string(s.state.Dump())

	assert.Equal(s.T(), big.NewInt(1),s.state.GetBalance(obj1.address))
	assert.Equal(s.T(), big.NewInt(2),s.state.GetBalance(obj2.address))

	want := `{
	"root": "87bb14a6e076efbad8c27788133c968def3e777a3eb4d8257b63f81d8f33229f",
	"accounts": {
		"0000000000000000000000000000000000000001": {
			"balance": "1",
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
