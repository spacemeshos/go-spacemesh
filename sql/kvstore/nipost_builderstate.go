package kvstore

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const nipostBuilderStateKey = "NIPostBuilderState"

// AddNIPostBuilderState adds the data for nipost builder state to the key-value store.
func AddNIPostBuilderState(db sql.Executor, state *types.NIPostBuilderState) error {
	return addKeyValue(db, nipostBuilderStateKey, state)
}

// GetNIPostBuilderState returns the data for nipost builder state from the key-value store.
func GetNIPostBuilderState(db sql.Executor) (*types.NIPostBuilderState, error) {
	res := &types.NIPostBuilderState{}
	if err := getKeyValue(db, nipostBuilderStateKey, res); err != nil {
		return nil, err
	}
	return res, nil
}

// ClearNIPostBuilderState clears the data for nipost builder state from the key-value store.
func ClearNIPostBuilderState(db sql.Executor) error {
	return clearKeyValue(db, nipostBuilderStateKey)
}
