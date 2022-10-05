package kvstore

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const nipostChallengeKey = "NIPost"

// AddNIPostChallenge adds the data for nipost to the key-value store.
func AddNIPostChallenge(db sql.Executor, ch *types.NIPostChallenge) error {
	return addKeyValue(db, nipostChallengeKey, ch)
}

// GetNIPostChallenge returns the data for nipost from the key-value store.
func GetNIPostChallenge(db sql.Executor) (*types.NIPostChallenge, error) {
	res := &types.NIPostChallenge{}
	if err := getKeyValue(db, nipostChallengeKey, res); err != nil {
		return nil, err
	}
	return res, nil
}

// ClearNIPostChallenge clears the data for nipost from the key-value store.
func ClearNIPostChallenge(db sql.Executor) error {
	return clearKeyValue(db, nipostChallengeKey)
}
