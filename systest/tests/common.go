package tests

//lint:file-ignore U1000 func waitAll is unused
import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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
	attempts = 3
)

func sendTransactions(ctx context.Context, eg *errgroup.Group, logger *zap.SugaredLogger, cl *cluster.Cluster, first, stop uint32, receiver types.Address, batch, amount int) error {
	for i := 0; i < cl.Accounts(); i++ {
		i := i
		client := cl.Client(i % cl.Total())
		nonce, err := getNonce(ctx, client, cl.Address(i))
		if err != nil {
			return fmt.Errorf("get nonce failed (%s:%s): %w", client.Name, cl.Address(i), err)
		}
		watchLayers(ctx, eg, client, func(layer *pb.LayerStreamResponse) (bool, error) {
			if layer.Layer.Number.Number == stop {
				return false, nil
			}
			if layer.Layer.Status != pb.Layer_LAYER_STATUS_APPROVED ||
				layer.Layer.Number.Number < first {
				return true, nil
			}
			// give some time for a previous layer to be applied
			// TODO(dshulyak) introduce api that simply subscribes to internal clock
			// and outputs events when the tick for the layer is available
			time.Sleep(200 * time.Millisecond)
			if nonce == 0 {
				logger.Infow("address needs to be spawned", "account", i)
				if err := submitSpawn(ctx, cl, i, client); err != nil {
					return false, fmt.Errorf("failed to spawn %w", err)
				}
				nonce++
				return true, nil
			}
			logger.Debugw("submitting transactions",
				"layer", layer.Layer.Number.Number,
				"client", client.Name,
				"account", i,
				"nonce", nonce,
				"batch", batch,
			)
			for j := 0; j < batch; j++ {
				// in case spawn isn't executed on this particular client
				retries := 3
				spendClient := client
				for k := 0; k < retries; k++ {
					err = submitSpend(ctx, cl, i, receiver, uint64(amount), nonce+uint64(j), spendClient)
					if err == nil {
						break
					}
					logger.Warnw("failed to spend", "client", spendClient.Name, "account", i, "nonce", nonce+uint64(j), "err", err.Error())
					spendClient = cl.Client((i + k + 1) % cl.Total())
				}
				if err != nil {
					return false, fmt.Errorf("spend failed %s %w", spendClient.Name, err)
				}
			}
			nonce += uint64(batch)
			return true, nil
		})
	}
	return nil
}

func submitTransaction(ctx context.Context, tx []byte, node *cluster.NodeClient) ([]byte, error) {
	txclient := pb.NewTransactionServiceClient(node)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	response, err := txclient.SubmitTransaction(ctx, &pb.SubmitTransactionRequest{Transaction: tx})
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
	collector func(*pb.GlobalStateStreamResponse) (bool, error),
) {
	eg.Go(func() error {
		return stateHashStream(ctx, node, collector)
	})
}

func stateHashStream(
	ctx context.Context,
	node *cluster.NodeClient,
	collector func(*pb.GlobalStateStreamResponse) (bool, error),
) error {
	stateapi := pb.NewGlobalStateServiceClient(node)
	states, err := stateapi.GlobalStateStream(ctx,
		&pb.GlobalStateStreamRequest{
			GlobalStateDataFlags: uint32(pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_GLOBAL_STATE_HASH),
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
	collector func(*pb.LayerStreamResponse) (bool, error),
) {
	eg.Go(func() error {
		return layersStream(ctx, node, collector)
	})
}

type layerCollector func(*pb.LayerStreamResponse) (bool, error)

func layersStream(ctx context.Context,
	node *cluster.NodeClient,
	collector layerCollector,
) error {
	meshapi := pb.NewMeshServiceClient(node)
	layers, err := meshapi.LayerStream(ctx, &pb.LayerStreamRequest{})
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
	svc := pb.NewMeshServiceClient(node)
	resp, err := svc.GenesisTime(ctx, &pb.GenesisTimeRequest{})
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

func waitLayer(ctx *testcontext.Context, node *cluster.NodeClient, lid uint32) error {
	svc := pb.NewMeshServiceClient(node)
	resp, err := svc.GenesisTime(ctx, &pb.GenesisTimeRequest{})
	if err != nil {
		return err
	}
	lyrTime := time.Unix(int64(resp.Unixtime.Value), 0).Add(time.Duration(lid) * testcontext.LayerDuration.Get(ctx.Parameters))

	now := time.Now()
	if !lyrTime.After(now) {
		return nil
	}
	ctx.Log.Debugw("waiting for layer", "now", now, "layer time", lyrTime, "layer", lid)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(lyrTime.Sub(now)):
		return nil
	}
}

func watchTransactionResults(ctx context.Context,
	eg *errgroup.Group,
	client *cluster.NodeClient,
	collector func(*pb.TransactionResult) (bool, error),
) {
	eg.Go(func() error {
		api := pb.NewTransactionServiceClient(client)
		rsts, err := api.StreamResults(ctx, &pb.TransactionResultsRequest{Watch: true})
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

func watchProposals(ctx context.Context, eg *errgroup.Group, client *cluster.NodeClient, collector func(*pb.Proposal) (bool, error)) {
	eg.Go(func() error {
		dbg := pb.NewDebugServiceClient(client)
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
	watchLayers(ctx, eg, client, func(layer *pb.LayerStreamResponse) (bool, error) {
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
	response, err := pb.NewMeshServiceClient(client).CurrentLayer(ctx, &pb.CurrentLayerRequest{})
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
	gstate := pb.NewGlobalStateServiceClient(client)
	resp, err := gstate.Account(ctx, &pb.AccountRequest{AccountId: &pb.AccountId{Address: address.String()}})
	if err != nil {
		return 0, err
	}
	return resp.AccountWrapper.StateProjected.Counter, nil
}

func submitSpawn(ctx context.Context, cluster *cluster.Cluster, account int, client *cluster.NodeClient) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := submitTransaction(ctx,
		wallet.SelfSpawn(cluster.Private(account), 0, sdk.WithGenesisID(cluster.GenesisID())),
		client)
	return err
}

func submitSpend(ctx context.Context, cluster *cluster.Cluster, account int, receiver types.Address, amount uint64, nonce uint64, client *cluster.NodeClient) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := submitTransaction(ctx,
		wallet.Spend(
			cluster.Private(account), receiver, amount,
			nonce,
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
	svc := pb.NewNodeServiceClient(node)
	resp, err := svc.Status(ctx, &pb.StatusRequest{})
	if err != nil {
		return false
	}
	return resp.Status.IsSynced
}

func getLayer(ctx context.Context, node *cluster.NodeClient, lid uint32) (*pb.Layer, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	layer := &pb.LayerNumber{Number: lid}
	msvc := pb.NewMeshServiceClient(node)
	lresp, err := msvc.LayersQuery(ctx, &pb.LayersQueryRequest{StartLayer: layer, EndLayer: layer})
	if err != nil {
		return nil, err
	}
	if len(lresp.Layer) != 1 {
		return nil, fmt.Errorf("request was made for one layer (%d)", layer.Number)
	}
	return lresp.Layer[0], nil
}

func getVerifiedLayer(ctx context.Context, node *cluster.NodeClient) (*pb.Layer, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	svc := pb.NewNodeServiceClient(node)
	resp, err := svc.Status(ctx, &pb.StatusRequest{})
	if err != nil {
		return nil, err
	}
	return getLayer(ctx, node, resp.Status.VerifiedLayer.Number)
}

func updatePoetServers(ctx context.Context, node *cluster.NodeClient, targets []string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	svc := pb.NewSmesherServiceClient(node)
	resp, err := svc.UpdatePoetServers(ctx, &pb.UpdatePoetServersRequest{Urls: targets})
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

	rst *pb.TransactionResult
}

func (r *txRequest) wait(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	client := pb.NewTransactionServiceClient(r.node)
	stream, err := client.StreamResults(ctx, &pb.TransactionResultsRequest{
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

func (r *txRequest) result(ctx context.Context) (*pb.TransactionResult, error) {
	if r.rst != nil {
		return r.rst, nil
	}
	client := pb.NewTransactionServiceClient(r.node)
	stream, err := client.StreamResults(ctx, &pb.TransactionResultsRequest{
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
