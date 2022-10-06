package kvstore

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const commitmentATXKey = "CommitmentATX"

// AddCommitmentATX adds the id for the commitment atx to the key-value store.
func AddCommitmentATX(db sql.Executor, atx types.ATXID) error {
	return addKeyValue(db, commitmentATXKey, &atx)
}

// GetCommitmentATX returns the id for the commitment atx from the key-value store.
func GetCommitmentATX(db sql.Executor) (types.ATXID, error) {
	var res types.ATXID
	if err := getKeyValue(db, commitmentATXKey, &res); err != nil {
		return *types.EmptyATXID, err
	}
	return res, nil
}

func ClearCommitmentAtx(db sql.Executor) error {
	return clearKeyValue(db, commitmentATXKey)
}
