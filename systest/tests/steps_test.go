package tests

import (
	"context"
	"encoding/hex"
	"math/rand"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/systest/chaos"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestStepCreate(t *testing.T) {
	ctx := testcontext.New(t)
	_, err := cluster.Reuse(ctx, cluster.WithKeys(10))
	require.NoError(t, err)
}

func TestStepChaos(t *testing.T) {
	tctx := testcontext.New(t)
	cl, err := cluster.Reuse(tctx, cluster.WithKeys(10))
	require.NoError(t, err)

	var (
		enableSkew = maxLayer(currentLayer(tctx, t, cl.Client(0))+2, 9)
		stopSkew   = enableSkew + 2
		skewOffset = "-3s" // hare round is 2s
	)
	tctx.Log.Debugw("enabled timeskew chaos",
		"enable", enableSkew,
		"stop", stopSkew,
	)

	failed := int(0.1 * float64(tctx.ClusterSize))
	eg, ctx := errgroup.WithContext(tctx)
	client := cl.Client(0)
	scheduleChaos(ctx, eg, client, enableSkew, stopSkew, func(ctx context.Context) (chaos.Teardown, error) {
		names := []string{}
		for i := 1; i <= failed; i++ {
			names = append(names, cl.Client(cl.Total()-i).Name)
		}
		return chaos.Timeskew(tctx, "skew", skewOffset, names...)
	})
}

func TestStepSubmitTransactions(t *testing.T) {
	tctx := testcontext.New(t)
	cl, err := cluster.Reuse(tctx, cluster.WithKeys(10))
	require.NoError(t, err)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	n := rng.Intn(cl.Accounts()-4) + 2 // send transaction from [2, 5] accounts

	tctx.Log.Debugw("submitting transactions", "accounts", n)

	// this test expected to tolerate clients failures.
	for i := 0; i < n; i++ {
		sender := cl.Address(i)
		private := cl.Private(i)
		client := cl.Client(i % cl.Total())

		log := tctx.Log.With(
			"origin", hex.EncodeToString(sender[:]),
			"client", client.Name,
		)

		for j := 0; j < rng.Intn(3)+1; j++ { // send between [1,3] transactions
			nonce, err := getNonce(tctx, client, sender)
			if err != nil {
				log.Errorw("failed to get nonce", "error", err)
				continue
			}
			var receiver [20]byte
			copy(receiver[:], cl.Address(rng.Intn(cl.Accounts())))
			amount := rng.Intn(10000)

			txlog := log.With(
				"nonce", nonce,
				"receiver", hex.EncodeToString(receiver[:]),
				"amount", amount,
			)

			submitter := newTransactionSubmitter(private, receiver, uint64(amount), nonce, client)
			ctx, cancel := context.WithTimeout(tctx, 3*time.Second)
			defer cancel()
			if err := submitter(ctx); err != nil {
				txlog.Error("failed to submit tx", "error", err)
				continue
			}
			txlog.Info("submitted tx")
		}
	}
}

func TestStepManageNodes(t *testing.T) {
	cctx := testcontext.New(t)
	cl, err := cluster.Reuse(cctx, cluster.WithKeys(10))
	require.NoError(t, err)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	delta := min(rng.Intn(cl.Total()*2/10), cctx.ClusterSize-cl.Total())

	log := cctx.Log.With(
		"current", cl.Total(),
		"max", cctx.ClusterSize,
		"delta", delta,
	)

	// TODO significant issue with this test, in case if node wasn't synced yet we may
	// remove it before it will even participate in the protocol
	// TODO doublecheck what will happen with persistent state for a pod in the stateful set
	// after it was removed
	if cl.Total() >= cctx.ClusterSize || rng.Intn(100) < 30 {
		log.Info("reducing cluster size")
		require.NoError(t, cl.DeleteSmeshers(cctx, delta))
	} else {
		log.Info("increasing cluster size")
		require.NoError(t, cl.AddSmeshers(cctx, delta))
	}
}

func TestStepVerifyBalances(t *testing.T) {
	cctx := testcontext.New(t)
	cl, err := cluster.Reuse(cctx, cluster.WithKeys(10))
	require.NoError(t, err)

	var (
		eg       errgroup.Group
		balances = make([]map[string]uint64, cl.Total())
	)
	for i := 0; i < cl.Total(); i++ {
		i := i
		client := cl.Client(i)
		eg.Go(func() error {
			state := map[string]uint64{}
			for j := 0; j < cl.Accounts(); j++ {
				addrbuf := cl.Address(j)
				ctx, cancel := context.WithTimeout(cctx, 2*time.Second)
				defer cancel()
				// TODO extend API to accept a layer, otherwise there concurrency problems
				balance, err := getAppliedBalance(ctx, client, addrbuf)
				if err != nil {
					cctx.Log.Errorw("requests to client are failing",
						"client", client.Name,
						"error", err,
					)
					return nil
				}
				address := hex.EncodeToString(addrbuf)

				state[address] = balance
			}
			balances[i] = state
			return nil
		})
	}
	require.NoError(t, eg.Wait())

	reference := balances[0]
	for addr, balance := range reference {
		cctx.Log.Infow("reference state",
			"address", addr,
			"balance", balance,
			"client", cl.Client(0).Name,
		)
	}
	for i := 1; i < cl.Total(); i++ {
		if balances[i] != nil {
			require.Equal(t, reference, balances[i], "client %d - %s", i, cl.Client(i).Name)
		}
	}

}
