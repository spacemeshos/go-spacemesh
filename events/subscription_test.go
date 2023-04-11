package events

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestSubscribe(t *testing.T) {
	InitializeReporter()
	t.Cleanup(CloseEventReporter)

	sub, err := Subscribe[Transaction]()
	require.NoError(t, err)
	lid := types.LayerID(10)
	tx := &types.Transaction{}
	tx.ID = types.TransactionID{1, 1, 1}
	ReportNewTx(lid, tx)

	select {
	case received := <-sub.Out():
		require.Equal(t, lid, received.LayerID)
		require.Equal(t, tx, received.Transaction)
	case <-time.After(time.Second):
		require.Fail(t, "timeout")
	}
}

func TestSubscribeFull(t *testing.T) {
	InitializeReporter()
	t.Cleanup(CloseEventReporter)

	sub, err := Subscribe[Transaction](WithBuffer(2))
	require.NoError(t, err)
	lid := types.LayerID(10)
	for i := 0; i < 3; i++ {
		tx := &types.Transaction{}
		tx.ID = types.TransactionID{1, 1, 1}
		ReportNewTx(lid, tx)
	}

	select {
	case <-sub.Full():
	case <-time.After(time.Second):
		require.Fail(t, "timeout")
	}
}
