package transactions

import "github.com/spacemeshos/go-spacemesh/common/types"

// ResultsFilter
type ResultsFilter struct {
	Addresses  []types.Address
	Start, End *types.LayerID
	TID        types.TransactionID
}

// ResultsIterator iterates over query
type ResultsIterator struct {
	filter ResultsFilter
}

func (ri *ResultsIterator) Next() (*types.TransactionResult, bool) {
	return nil, false
}
