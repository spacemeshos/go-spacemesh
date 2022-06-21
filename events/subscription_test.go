package events

import (
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/require"
)

func TestSubscribe(t *testing.T) {
	InitializeReporter()
	t.Cleanup(CloseEventReporter)

	sub, err := Subscribe[Transaction]()
	require.NoError(t, err)
	lid := types.NewLayerID(10)
	tx := &types.Transaction{}
	tx.ID = types.TransactionID{1, 1, 1}
	ReportNewTx(lid, tx)

	received := <-sub.Out()
	require.Equal(t, lid, received.LayerID)
	require.Equal(t, tx, received.Transaction)
}
