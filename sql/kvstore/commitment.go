package kvstore

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const commitmentATXKey = "commitmentATX"

func getKeyForNode(nodeId types.NodeID) string {
	return fmt.Sprintf("%s-%s", commitmentATXKey, nodeId)
}

// AddCommitmentATXForNode adds the id for the commitment atx to the key-value store.
func AddCommitmentATXForNode(db sql.Executor, atx types.ATXID, nodeId types.NodeID) error {
	return addKeyValue(db, getKeyForNode(nodeId), &atx)
}

// GetCommitmentATXForNode returns the id for the commitment atx from the key-value store.
func GetCommitmentATXForNode(db sql.Executor, nodeId types.NodeID) (types.ATXID, error) {

	var res types.ATXID
	if err := getKeyValue(db, getKeyForNode(nodeId), &res); err != nil {
		return *types.EmptyATXID, err
	}
	return res, nil
}

func ClearCommitmentAtx(db sql.Executor, nodeId types.NodeID) error {
	return clearKeyValue(db, getKeyForNode(nodeId))
}
