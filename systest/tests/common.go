package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	spacemeshv1 "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/ed25519"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/systest/chaos"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

func submitTransacition(ctx context.Context, tx []byte, node *cluster.NodeClient) error {
	txclient := spacemeshv1.NewTransactionServiceClient(node)
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	response, err := txclient.SubmitTransaction(ctx, &spacemeshv1.SubmitTransactionRequest{Transaction: tx})
	if err != nil {
		return err
	}
	if response.Txstate == nil {
		return fmt.Errorf("tx state should not be nil")
	}
	return nil
}

func extractNames(nodes ...*cluster.NodeClient) []string {
	var rst []string
	for _, n := range nodes {
		rst = append(rst, n.Name)
	}
	return rst
}

func watchLayers(ctx context.Context, eg *errgroup.Group,
	client *cluster.NodeClient,
	collector func(*spacemeshv1.LayerStreamResponse) (bool, error),
) {
	eg.Go(func() error {
		meshapi := spacemeshv1.NewMeshServiceClient(client)
		layers, err := meshapi.LayerStream(ctx, &spacemeshv1.LayerStreamRequest{})
		if err != nil {
			return err
		}
		for {
			layer, err := layers.Recv()
			if err != nil {
				return err
			}
			if cont, err := collector(layer); !cont {
				return err
			}
		}
	})
}

func watchTransactionResults(ctx context.Context,
	eg *errgroup.Group,
	client *cluster.NodeClient,
	collector func(*spacemeshv1.TransactionResult) (bool, error),
) {
	eg.Go(func() error {
		api := spacemeshv1.NewTransactionServiceClient(client)
		rsts, err := api.StreamResults(ctx, &spacemeshv1.TransactionResultsRequest{Watch: true})
		if err != nil {
			return err
		}
		for {
			rst, err := rsts.Recv()
			if err != nil {
				return fmt.Errorf("stream error on receiving result %s: %w", client.Name, err)
			}
			if cont, err := collector(rst); !cont {
				return err
			}
		}
	})
}

func watchProposals(ctx context.Context, eg *errgroup.Group, client *cluster.NodeClient, collector func(*spacemeshv1.Proposal) (bool, error)) {
	eg.Go(func() error {
		dbg := spacemeshv1.NewDebugServiceClient(client)
		proposals, err := dbg.ProposalsStream(ctx, &empty.Empty{})
		if err != nil {
			return fmt.Errorf("proposal stream for %s: %w", client.Name, err)
		}
		for {
			proposal, err := proposals.Recv()
			if err != nil {
				return fmt.Errorf("proposal event for %s: %w", client.Name, err)
			}
			if cont, err := collector(proposal); !cont {
				return err
			}
		}
	})
}

func prettyHex(buf []byte) string {
	return fmt.Sprintf("0x%x", buf)
}

func scheduleChaos(ctx context.Context, eg *errgroup.Group, client *cluster.NodeClient, from, to uint32, action func(context.Context) (chaos.Teardown, error)) {
	var teardown chaos.Teardown
	watchLayers(ctx, eg, client, func(layer *spacemeshv1.LayerStreamResponse) (bool, error) {
		if layer.Layer.Number.Number == from && teardown == nil {
			var err error
			teardown, err = action(ctx)
			if err != nil {
				return false, err
			}
		}
		if layer.Layer.Number.Number == to {
			if err := teardown(ctx); err != nil {
				return false, err
			}
			return false, nil
		}
		return true, nil
	})
}

func currentLayer(ctx context.Context, tb testing.TB, client *cluster.NodeClient) uint32 {
	tb.Helper()
	response, err := spacemeshv1.NewMeshServiceClient(client).CurrentLayer(ctx, &spacemeshv1.CurrentLayerRequest{})
	require.NoError(tb, err)
	return response.Layernum.Number
}

func waitAll(tctx *testcontext.Context, cl *cluster.Cluster) error {
	var eg errgroup.Group
	for i := 0; i < cl.Total(); i++ {
		i := i
		eg.Go(func() error {
			return cl.Wait(tctx, i)
		})
	}
	return eg.Wait()
}

func min(i, j int) int {
	if i < j {
		return i
	}
	return j
}

func maxLayer(i, j uint32) uint32 {
	if i > j {
		return i
	}
	return j
}

func nextFirstLayer(current uint32, size uint32) uint32 {
	if over := current % size; over != 0 {
		current += size - over
	}
	return current
}

func getNonce(ctx context.Context, client *cluster.NodeClient, address []byte) (uint64, error) {
	gstate := spacemeshv1.NewGlobalStateServiceClient(client)
	resp, err := gstate.Account(ctx, &spacemeshv1.AccountRequest{AccountId: &spacemeshv1.AccountId{Address: address}})
	if err != nil {
		return 0, err
	}
	return resp.AccountWrapper.StateProjected.Counter, nil
}

func submitSpawn(ctx context.Context, cluster *cluster.Cluster, account int, client *cluster.NodeClient) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return submitTransacition(ctx, wallet.SelfSpawn(cluster.Private(account)), client)
}

func submitSpend(ctx context.Context, pk ed25519.PrivateKey, receiver [20]byte, amount uint64, nonce uint64, client *cluster.NodeClient) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return submitTransacition(ctx,
		wallet.Spend(
			signing.PrivateKey(pk), types.Address(receiver), amount,
			types.Nonce{Counter: nonce},
		),
		client)
}
