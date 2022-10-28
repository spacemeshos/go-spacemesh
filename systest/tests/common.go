package tests

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	spacemeshv1 "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"google.golang.org/genproto/googleapis/rpc/code"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/systest/chaos"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

const (
	attempts       = 3
	layersPerEpoch = 4
)

func submitTransaction(ctx context.Context, tx []byte, node *cluster.NodeClient) ([]byte, error) {
	txclient := spacemeshv1.NewTransactionServiceClient(node)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	response, err := txclient.SubmitTransaction(ctx, &spacemeshv1.SubmitTransactionRequest{Transaction: tx})
	if err != nil {
		return nil, err
	}
	if response.Txstate == nil {
		return nil, fmt.Errorf("tx state should not be nil")
	}
	return response.Txstate.Id.Id, nil
}

func watchStateHashes(
	ctx context.Context,
	eg *errgroup.Group,
	node *cluster.NodeClient,
	collector func(*spacemeshv1.GlobalStateStreamResponse) (bool, error),
) {
	eg.Go(func() error {
		return stateHashStream(ctx, node, collector)
	})
}

func stateHashStream(
	ctx context.Context,
	node *cluster.NodeClient,
	collector func(*spacemeshv1.GlobalStateStreamResponse) (bool, error),
) error {
	stateapi := spacemeshv1.NewGlobalStateServiceClient(node)
	states, err := stateapi.GlobalStateStream(ctx,
		&spacemeshv1.GlobalStateStreamRequest{
			GlobalStateDataFlags: uint32(spacemeshv1.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_GLOBAL_STATE_HASH),
		})
	if err != nil {
		return err
	}
	for {
		state, err := states.Recv()
		if err != nil {
			return fmt.Errorf("stream err from client %v: %w", node.Name, err)
		}
		if cont, err := collector(state); !cont {
			return err
		}
	}
}

func watchLayers(ctx context.Context, eg *errgroup.Group,
	node *cluster.NodeClient,
	collector func(*spacemeshv1.LayerStreamResponse) (bool, error),
) {
	eg.Go(func() error {
		return layersStream(ctx, node, collector)
	})
}

func layersStream(ctx context.Context,
	node *cluster.NodeClient,
	collector func(*spacemeshv1.LayerStreamResponse) (bool, error),
) error {
	meshapi := spacemeshv1.NewMeshServiceClient(node)
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
}

func waitGenesis(ctx *testcontext.Context, node *cluster.NodeClient) error {
	svc := spacemeshv1.NewMeshServiceClient(node)
	resp, err := svc.GenesisTime(ctx, &spacemeshv1.GenesisTimeRequest{})
	if err != nil {
		return err
	}
	genesis := time.Unix(int64(resp.Unixtime.Value), 0)
	now := time.Now()
	if !genesis.After(now) {
		return nil
	}
	ctx.Log.Debugw("waiting for genesis", "now", now, "genesis", genesis)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(genesis.Sub(now)):
		return nil
	}
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

func getNonce(ctx context.Context, client *cluster.NodeClient, address types.Address) (uint64, error) {
	gstate := spacemeshv1.NewGlobalStateServiceClient(client)
	resp, err := gstate.Account(ctx, &spacemeshv1.AccountRequest{AccountId: &spacemeshv1.AccountId{Address: address.String()}})
	if err != nil {
		return 0, err
	}
	return resp.AccountWrapper.StateProjected.Counter, nil
}

func submitSpawn(ctx context.Context, cluster *cluster.Cluster, account int, client *cluster.NodeClient) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := submitTransaction(ctx,
		wallet.SelfSpawn(cluster.Private(account), types.Nonce{}, sdk.WithGenesisID(cluster.GenesisID())),
		client)
	return err
}

func submitSpend(ctx context.Context, cluster *cluster.Cluster, account int, receiver types.Address, amount uint64, nonce uint64, client *cluster.NodeClient) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := submitTransaction(ctx,
		wallet.Spend(
			cluster.Private(account), receiver, amount,
			types.Nonce{Counter: nonce},
			sdk.WithGenesisID(cluster.GenesisID()),
		),
		client)
	return err
}

func syncedNodes(ctx context.Context, cl *cluster.Cluster) []*cluster.NodeClient {
	var synced []*cluster.NodeClient
	for i := 0; i < cl.Total(); i++ {
		if !isSynced(ctx, cl.Client(i)) {
			continue
		}
		synced = append(synced, cl.Client(i))
	}
	return synced
}

func isSynced(ctx context.Context, node *cluster.NodeClient) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	svc := spacemeshv1.NewNodeServiceClient(node)
	resp, err := svc.Status(ctx, &spacemeshv1.StatusRequest{})
	if err != nil {
		return false
	}
	return resp.Status.IsSynced
}

func getLayer(ctx context.Context, node *cluster.NodeClient, lid uint32) (*spacemeshv1.Layer, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	layer := &spacemeshv1.LayerNumber{Number: lid}
	msvc := spacemeshv1.NewMeshServiceClient(node)
	lresp, err := msvc.LayersQuery(ctx, &spacemeshv1.LayersQueryRequest{StartLayer: layer, EndLayer: layer})
	if err != nil {
		return nil, err
	}
	if len(lresp.Layer) != 1 {
		return nil, fmt.Errorf("request was made for one layer (%d)", layer.Number)
	}
	return lresp.Layer[0], nil
}

func getVerifiedLayer(ctx context.Context, node *cluster.NodeClient) (*spacemeshv1.Layer, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	svc := spacemeshv1.NewNodeServiceClient(node)
	resp, err := svc.Status(ctx, &spacemeshv1.StatusRequest{})
	if err != nil {
		return nil, err
	}
	return getLayer(ctx, node, resp.Status.VerifiedLayer.Number)
}

func updatePoetServers(ctx context.Context, node *cluster.NodeClient, targets []string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	svc := spacemeshv1.NewNodeServiceClient(node)
	resp, err := svc.UpdatePoetServers(ctx, &spacemeshv1.UpdatePoetServersRequest{Urls: targets})
	if err != nil {
		return false, err
	}
	return resp.Status.Code == int32(code.Code_OK), nil
}

type txClient struct {
	account cluster.Account
	node    *cluster.NodeClient
}

func (c *txClient) nonce(ctx context.Context) (uint64, error) {
	return getNonce(ctx, c.node, c.account.Address)
}

func (c *txClient) submit(ctx context.Context, tx []byte) (*txRequest, error) {
	var (
		txid []byte
		err  error
	)
	for i := 0; i < attempts; i++ {
		if txid, err = submitTransaction(ctx, tx, c.node); err == nil {
			return &txRequest{
				node: c.node,
				txid: txid,
			}, nil
		}
	}
	return nil, fmt.Errorf("submit to node %s: %w", c.node.Name, err)
}

type txRequest struct {
	node *cluster.NodeClient
	txid []byte

	rst *spacemeshv1.TransactionResult
}

func (r *txRequest) wait(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	client := spacemeshv1.NewTransactionServiceClient(r.node)
	stream, err := client.StreamResults(ctx, &spacemeshv1.TransactionResultsRequest{
		Id:    r.txid,
		Watch: true,
	})
	if err != nil {
		return err
	}
	rst, err := stream.Recv()
	if err != nil {
		return err
	}
	r.rst = rst
	return nil
}

func (r *txRequest) result(ctx context.Context) (*spacemeshv1.TransactionResult, error) {
	if r.rst != nil {
		return r.rst, nil
	}
	client := spacemeshv1.NewTransactionServiceClient(r.node)
	stream, err := client.StreamResults(ctx, &spacemeshv1.TransactionResultsRequest{
		Id: r.txid,
	})
	if err != nil {
		return nil, err
	}
	rst, err := stream.Recv()
	if err != nil {
		// eof without result - transaction wasn't applied yet
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, err
	}
	r.rst = rst
	return rst, nil
}
