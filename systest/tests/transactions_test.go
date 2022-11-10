package tests

import (
	"encoding/hex"
	"testing"

	spacemeshv1 "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

func testTransactions(t *testing.T, tctx *testcontext.Context, cl *cluster.Cluster) {
	var (
		// start sending transactions after two layers or after genesis
		first              = maxLayer(currentLayer(tctx, t, cl.Client(0))+2, 8)
		sendFor     uint32 = 8
		stopSending        = first + sendFor
		stopWaiting        = stopSending + 4
		batch              = 10
		amount             = 100

		// each account creates spawn transaction in the first layer
		// plus batch number of spend transactions in every layer after that
		expectedCount = cl.Accounts() * (1 + int(sendFor-1)*batch)
	)
	tctx.Log.Debugw("running transactions test",
		"from", first,
		"stop sending", stopSending,
		"stop waiting", stopWaiting,
		"expected transactions", expectedCount,
	)
	receiver := types.GenerateAddress([]byte{11, 1, 1})
	state := spacemeshv1.NewGlobalStateServiceClient(cl.Client(0))
	response, err := state.Account(tctx, &spacemeshv1.AccountRequest{AccountId: &spacemeshv1.AccountId{Address: receiver.String()}})
	require.NoError(t, err)
	before := response.AccountWrapper.StateCurrent.Balance

	eg, ctx := errgroup.WithContext(tctx)
	sendTransactions(ctx, eg, tctx.Log, cl, first, stopSending)
	txs := make([][]*spacemeshv1.Transaction, cl.Total())

	for i := 0; i < cl.Total(); i++ {
		i := i
		client := cl.Client(i)
		watchTransactionResults(tctx.Context, eg, client, func(rst *spacemeshv1.TransactionResult) (bool, error) {
			txs[i] = append(txs[i], rst.Tx)
			count := len(txs[i])
			tctx.Log.Debugw("received transaction client",
				"layer", rst.Layer,
				"client", client.Name,
				"tx", "0x"+hex.EncodeToString(rst.Tx.Id),
				"count", count,
			)
			return len(txs[i]) < expectedCount, nil
		})
	}
	require.NoError(t, eg.Wait())

	reference := txs[0]
	for _, tested := range txs[1:] {
		require.Len(t, tested, len(reference))
		for i := range reference {
			require.Equal(t, reference[i], tested[i])
		}
	}

	diff := batch * amount * int(sendFor-1) * cl.Accounts()
	for i := 0; i < cl.Total(); i++ {
		client := cl.Client(i)
		state := spacemeshv1.NewGlobalStateServiceClient(client)
		response, err := state.Account(tctx, &spacemeshv1.AccountRequest{AccountId: &spacemeshv1.AccountId{Address: receiver.String()}})
		require.NoError(t, err)
		after := response.AccountWrapper.StateCurrent.Balance
		tctx.Log.Debugw("receiver state",
			"before", before.Value,
			"after", after.Value,
			"expected-diff", diff,
			"diff", after.Value-before.Value,
		)
		require.Equal(t, int(before.Value)+diff,
			int(response.AccountWrapper.StateCurrent.Balance.Value), "client=%s", client.Name)
	}
}
