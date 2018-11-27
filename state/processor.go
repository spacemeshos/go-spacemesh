package state

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"math/big"
)

type Transaction struct {
	AccountNonce uint64
	Price        *big.Int
	GasLimit     uint64
	Recipient    *common.Address
	Amount       *big.Int
	Payload      []byte

	//todo: add signatures

	Hash *common.Hash
}


type PseudoRandomizer interface {
	Seed(seed uint64)
	NextInt32() uint32
	NextInt64() uint64
}


type TransactionProcessor struct {
	rand PseudoRandomizer
	globalState GlobalStateDB


}

//should receive sort predicate
func (tp *TransactionProcessor) SortAndMerge(layer *mesh.Layer)(transactions []*Transaction){
	//call sort
	//call merge
	//eliminate doubles
	//check for double spends? (how do i do this? check the nonce against the prev one?
	return nil
}


func Process(transactions []*Transaction){
	for _, trans := range transactions {
		ApplyTransaction(trans)
	}

}

func ApplyTransaction(trans *Transaction){

	//check if dst account exists
	//check if src exist
	//check if src account has enough funds
	//set journal backup
	//add 1 to account nonce
	//verify current nonce
	//upate accounts accordingly
	//error if no funds
	//commit to tree
}