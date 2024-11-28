package tests

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

func testTransactions(
	tb testing.TB,
	tctx *testcontext.Context,
	cl *cluster.Cluster,
	sendFor uint32,
) {
	var (
		// start sending transactions after two layers or after genesis
		first  = max(currentLayer(tctx, tb, cl.Client(0))+2, 8)
		stop   = first + sendFor
		batch  = 10
		amount = 100

		// each account creates spawn transaction in the first layer
		// plus batch number of spend transactions in every layer after that
		expectedCount = cl.Accounts() * (1 + int(sendFor-1)*batch)
	)
	tctx.Log.Debugw("running transactions test",
		"from", first,
		"stop sending", stop,
		"expected transactions", expectedCount,
	)
	receiver := types.GenerateAddress([]byte{11, 1, 1})
	state := pb.NewGlobalStateServiceClient(cl.Client(0).PubConn())
	response, err := state.Account(
		tctx,
		&pb.AccountRequest{AccountId: &pb.AccountId{Address: receiver.String()}},
	)
	require.NoError(tb, err)
	before := response.AccountWrapper.StateCurrent.Balance

	layerDuration := testcontext.LayerDuration.Get(tctx.Parameters)
	layersPerEpoch := uint32(testcontext.LayersPerEpoch.Get(tctx.Parameters))
	deadline := cl.Genesis().Add(time.Duration(stop+2*layersPerEpoch) * layerDuration) // add 2 epochs of buffer
	ctx, cancel := context.WithDeadline(tctx, deadline)
	defer cancel()
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return sendTransactions(ctx, tctx.Log.Desugar(), cl, first, stop, receiver, batch, amount)
	})
	txs := make([][]*pb.Transaction, cl.Total())

	for i := range cl.Total() {
		client := cl.Client(i)
		eg.Go(func() error {
			return watchTransactionResults(ctx, client, tctx.Log.Desugar(),
				func(rst *pb.TransactionResult) (bool, error) {
					txs[i] = append(txs[i], rst.Tx)
					count := len(txs[i])
					tctx.Log.Debugw("received transaction client",
						"layer", rst.Layer,
						"client", client.Name,
						"tx", "0x"+hex.EncodeToString(rst.Tx.Id),
						"count", count,
					)
					return len(txs[i]) < expectedCount, nil
				},
			)
		})
	}
	require.NoError(tb, eg.Wait())

	reference := txs[0]
	for i, tested := range txs[1:] {
		require.Len(tb, tested, len(reference))
		for j := range reference {
			require.Equal(tb, reference[j], tested[j], cl.Client(i+1).Name)
		}
	}

	diff := batch * amount * int(sendFor-1) * cl.Accounts()
	for i := 0; i < cl.Total(); i++ {
		client := cl.Client(i)
		state := pb.NewGlobalStateServiceClient(client.PubConn())
		response, err := state.Account(
			tctx,
			&pb.AccountRequest{AccountId: &pb.AccountId{Address: receiver.String()}},
		)
		require.NoError(tb, err)
		after := response.AccountWrapper.StateCurrent.Balance
		tctx.Log.Infow("receiver state",
			"before", before.Value,
			"after", after.Value,
			"expected-diff", diff,
			"diff", after.Value-before.Value,
		)
		require.Equal(tb, int(before.Value)+diff,
			int(response.AccountWrapper.StateCurrent.Balance.Value), "client=%s", client.Name)
	}
}
