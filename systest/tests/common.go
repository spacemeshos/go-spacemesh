package tests

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	pb2 "github.com/spacemeshos/api/release/go/spacemesh/v2beta1"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

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

var retryBackoff = 10 * time.Second

func sendTransactions(
	ctx context.Context,
	logger *zap.Logger,
	cl *cluster.Cluster,
	first, stop uint32,
	receiver types.Address,
	batch, amount int,
) error {
	eg, ctx := errgroup.WithContext(ctx)
	for i := range cl.Accounts() {
		client := cl.Client(i % cl.Total())
		nonce, err := getNonce(ctx, client, cl.Address(i))
		if err != nil {
			return fmt.Errorf("get nonce failed (%s: %s): %w", client.Name, cl.Address(i).String(), err)
		}
		watchLayers(ctx, eg, client, logger, func(layer *pb2.Layer) (bool, error) {
			if layer.Number < first {
				return true, nil
			}
			if layer.Number >= stop {
				return false, nil
			}
			if layer.Status != pb2.Layer_LAYER_STATUS_APPLIED {
				return true, nil
			}
			// give some time for a previous layer to be applied
			// TODO(dshulyak) introduce api that simply subscribes to internal clock
			// and outputs events when the tick for the layer is available
			time.Sleep(200 * time.Millisecond)
			if nonce == 0 {
				logger.Info("address needs to be spawned",
					zap.String("client", client.Name),
					zap.Stringer("address", cl.Address(i)),
				)
				ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				if _, err := submitTransaction(ctx,
					wallet.SelfSpawn(cl.Private(i), 0, sdk.WithGenesisID(cl.GenesisID())),
					client,
				); err != nil {
					return false, fmt.Errorf("failed to spawn %w", err)
				}
				nonce++
				return true, nil
			}
			logger.Debug("submitting transactions",
				zap.Uint32("layer", layer.Number),
				zap.String("client", client.Name),
				zap.Stringer("address", cl.Address(i)),
				zap.Uint64("nonce", nonce),
				zap.Int("batch", batch),
			)
			for j := range batch {
				var err error
				for range 3 { // retry on failure 3 times
					err = submitSpend(ctx, cl, i, receiver, uint64(amount), nonce+uint64(j), client)
					if err == nil {
						break
					}
					logger.Warn("failed to spend",
						zap.String("client", client.Name),
						zap.Stringer("address", cl.Address(i)),
						zap.Uint64("nonce", nonce+uint64(j)),
						zap.Error(err),
					)
					time.Sleep(1 * time.Second) // wait before retrying
				}
				if err != nil {
					return false, fmt.Errorf("spend failed %s %w", client.Name, err)
				}
			}
			nonce += uint64(batch)
			return true, nil
		})
	}
	return eg.Wait()
}

func submitTransaction(ctx context.Context, tx []byte, node *cluster.NodeClient) ([]byte, error) {
	client := pb2.NewTransactionServiceClient(node.PubConn())
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := client.SubmitTransaction(ctx, &pb2.SubmitTransactionRequest{Transaction: tx})
	if err != nil {
		return nil, err
	}
	if resp.TxId == nil {
		return nil, errors.New("tx id should not be nil")
	}
	return resp.TxId, nil
}

func watchLayers(
	ctx context.Context,
	eg *errgroup.Group,
	node *cluster.NodeClient,
	logger *zap.Logger,
	collector func(*pb2.Layer) (bool, error),
) {
	eg.Go(func() error {
		return layersStream(ctx, node, logger, collector)
	})
}

func layersStream(
	ctx context.Context,
	node *cluster.NodeClient,
	logger *zap.Logger,
	collector func(*pb2.Layer) (bool, error),
) error {
	retries := 0
BACKOFF:
	client := pb2.NewLayerStreamServiceClient(node.PrivConn())
	stream, err := client.Stream(ctx, &pb2.LayerStreamRequest{
		Watch: true,
	})
	if err != nil {
		return fmt.Errorf("streaming layers for %s: %w", node.Name, err)
	}
	defer stream.CloseSend()
	for {
		layer, err := stream.Recv()
		s, ok := status.FromError(err)
		if !ok {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("unknown error: %w", err)
		}
		switch s.Code() {
		case codes.OK:
			if cont, err := collector(layer); !cont {
				return err
			}
		case codes.Canceled, codes.DeadlineExceeded:
			return nil
		case codes.Unavailable:
			if retries == attempts {
				return errors.New("layer stream unavailable")
			}
			retries++
			time.Sleep(retryBackoff)
			goto BACKOFF
		default:
			logger.Warn(
				"layer stream error",
				zap.String("client", node.Name),
				zap.Error(err),
				zap.Any("status", s),
			)
			return fmt.Errorf("stream err from client %v: %w", node.Name, err)
		}
	}
}

func malfeasanceStream(
	ctx context.Context,
	node *cluster.NodeClient,
	logger *zap.Logger,
	collector func(*pb2.MalfeasanceProof) (bool, error),
) error {
	retries := 0
BACKOFF:
	client := pb2.NewMalfeasanceStreamServiceClient(node.PrivConn())
	proofs, err := client.Stream(ctx, &pb2.MalfeasanceStreamRequest{
		Watch: true,
	})
	if err != nil {
		return fmt.Errorf("streaming malfeasance for %s: %w", node.Name, err)
	}
	defer proofs.CloseSend()
	for {
		proof, err := proofs.Recv()
		s, ok := status.FromError(err)
		if !ok {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("unknown error: %w", err)
		}
		switch s.Code() {
		case codes.OK:
			if cont, err := collector(proof); !cont {
				return err
			}
		case codes.Canceled, codes.DeadlineExceeded:
			return nil
		case codes.Unavailable:
			if retries == attempts {
				return errors.New("malfeasance stream unavailable")
			}
			proofs.CloseSend()
			retries++
			time.Sleep(retryBackoff)
			goto BACKOFF
		default:
			logger.Warn(
				"malfeasance stream error",
				zap.String("client", node.Name),
				zap.Error(err),
				zap.Any("status", s),
			)
			return fmt.Errorf("stream err from client %v: %w", node.Name, err)
		}
	}
}

func waitTransaction(ctx context.Context, eg *errgroup.Group, node *cluster.NodeClient, id []byte) {
	eg.Go(func() error {
		client := pb2.NewTransactionStreamServiceClient(node.PrivConn())
		stream, err := client.Stream(ctx, &pb2.TransactionStreamRequest{
			Watch: true,
			Txid:  [][]byte{id},
		})
		if err != nil {
			return err
		}
		defer stream.CloseSend()
		_, err = stream.Recv()
		if err != nil {
			return fmt.Errorf("stream error on receiving result %s: %w", node.Name, err)
		}
		return nil
	})
}

func watchTransactionResults(
	ctx context.Context,
	node *cluster.NodeClient,
	log *zap.Logger,
	collector func(*pb2.TransactionResponse) (bool, error),
) error {
	retries := 0
BACKOFF:
	client := pb2.NewTransactionStreamServiceClient(node.PrivConn())
	stream, err := client.Stream(ctx, &pb2.TransactionStreamRequest{
		Watch: true,
	})
	if err != nil {
		return fmt.Errorf("streaming transactions for %s: %w", node.Name, err)
	}
	defer stream.CloseSend()
	for {
		rst, err := stream.Recv()
		s, ok := status.FromError(err)
		if !ok {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("unknown error: %w", err)
		}
		switch s.Code() {
		case codes.OK:
			if cont, err := collector(rst); !cont {
				return err
			}
		case codes.Canceled, codes.DeadlineExceeded:
			return nil
		case codes.Unavailable:
			if retries == attempts {
				return errors.New("transaction results unavailable")
			}
			retries++
			time.Sleep(retryBackoff)
			goto BACKOFF
		default:
			log.Warn(
				"transactions stream error",
				zap.String("client", node.Name),
				zap.Error(err),
				zap.Any("status", s),
			)
			return fmt.Errorf("stream error on receiving result %s: %w", node.Name, err)
		}
	}
}

func watchProposals(
	ctx context.Context,
	eg *errgroup.Group,
	node *cluster.NodeClient,
	log *zap.Logger,
	collector func(*pb.Proposal) (bool, error),
) {
	eg.Go(func() error {
		retries := 0
	BACKOFF:
		client := pb.NewDebugServiceClient(node.PrivConn())
		stream, err := client.ProposalsStream(ctx, &emptypb.Empty{})
		if err != nil {
			return fmt.Errorf("streaming proposals for %s: %w", node.Name, err)
		}
		defer stream.CloseSend()
		for {
			proposal, err := stream.Recv()
			s, ok := status.FromError(err)
			if !ok {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("unknown error: %w", err)
			}
			switch s.Code() {
			case codes.OK:
				if cont, err := collector(proposal); !cont {
					return err
				}
			case codes.Canceled, codes.DeadlineExceeded:
				return nil
			case codes.Unavailable:
				if retries == attempts {
					return errors.New("proposal stream unavailable")
				}
				retries++
				time.Sleep(retryBackoff)
				goto BACKOFF
			default:
				log.Warn(
					"proposals stream error",
					zap.String("client", node.Name),
					zap.Error(err),
					zap.Any("status", s),
				)
				return fmt.Errorf("proposal event for %s: %w", node.Name, err)
			}
		}
	})
}

func prettyHex(buf []byte) string {
	return fmt.Sprintf("0x%x", buf)
}

func scheduleChaos(
	ctx context.Context,
	eg *errgroup.Group,
	client *cluster.NodeClient,
	logger *zap.Logger,
	from, to uint32,
	action func(context.Context) (chaos.Teardown, error),
) {
	var teardown chaos.Teardown
	watchLayers(ctx, eg, client, logger, func(layer *pb2.Layer) (bool, error) {
		switch {
		case layer.Number < from:
			return true, nil
		case layer.Number == from && teardown == nil:
			var err error
			teardown, err = action(ctx)
			if err != nil {
				return false, err
			}
		case layer.Number >= to:
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
	resp, err := pb2.NewNodeServiceClient(client.PubConn()).Status(ctx, &pb2.NodeStatusRequest{})
	require.NoError(tb, err)
	return resp.CurrentLayer
}

func waitAll(tctx *testcontext.Context, cl *cluster.Cluster) error {
	var eg errgroup.Group
	for i := range cl.Total() {
		eg.Go(func() error {
			return cl.Wait(tctx, i)
		})
	}
	return eg.Wait()
}

func nextFirstLayer(current, size uint32) uint32 {
	if over := current % size; over != 0 {
		current += size - over
	}
	return current
}

func getNonce(ctx context.Context, node *cluster.NodeClient, address types.Address) (uint64, error) {
	resp, err := pb2.NewAccountServiceClient(node.PubConn()).List(ctx, &pb2.AccountRequest{
		Addresses: []string{address.String()},
		Limit:     1,
	})
	if err != nil {
		return 0, err
	}
	return resp.Accounts[0].Current.Counter, nil
}

func currentBalance(ctx context.Context, node *cluster.NodeClient, address types.Address) (uint64, error) {
	resp, err := pb2.NewAccountServiceClient(node.PubConn()).List(ctx, &pb2.AccountRequest{
		Addresses: []string{address.String()},
	})
	if err != nil {
		return 0, err
	}
	return resp.Accounts[0].Current.Balance, nil
}

func submitSpend(
	ctx context.Context,
	cluster *cluster.Cluster,
	account int,
	receiver types.Address,
	amount, nonce uint64,
	client *cluster.NodeClient,
) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx := wallet.Spend(cluster.Private(account), receiver, amount, nonce, sdk.WithGenesisID(cluster.GenesisID()))
	_, err := submitTransaction(ctx, tx, client)
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
	svc := pb2.NewNodeServiceClient(node.PubConn())
	resp, err := svc.Status(ctx, &pb2.NodeStatusRequest{})
	if err != nil {
		return false
	}
	return resp.Status == pb2.NodeStatusResponse_SYNC_STATUS_SYNCED
}

func getLayer(ctx context.Context, node *cluster.NodeClient, lid uint32) (*pb2.Layer, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client := pb2.NewLayerServiceClient(node.PubConn())
	resp, err := client.List(ctx, &pb2.LayerRequest{StartLayer: lid, EndLayer: lid})
	if err != nil {
		return nil, err
	}
	if len(resp.Layers) != 1 {
		return nil, fmt.Errorf("request was made for one layer (%d)", lid)
	}
	return resp.Layers[0], nil
}

func getVerifiedLayer(ctx context.Context, node *cluster.NodeClient) (*pb2.Layer, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client := pb2.NewNodeServiceClient(node.PubConn())
	resp, err := client.Status(ctx, &pb2.NodeStatusRequest{})
	if err != nil {
		return nil, err
	}
	return getLayer(ctx, node, resp.AppliedLayer)
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

	rst *pb2.TransactionResponse
}

func (r *txRequest) wait(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	client := pb2.NewTransactionStreamServiceClient(r.node.PrivConn())
	stream, err := client.Stream(ctx, &pb2.TransactionStreamRequest{
		Txid:  [][]byte{r.txid},
		Watch: true,
	})
	if err != nil {
		return err
	}
	defer stream.CloseSend()
	rst, err := stream.Recv()
	if err != nil {
		return err
	}
	r.rst = rst
	return nil
}

func (r *txRequest) result(ctx context.Context) (*pb2.TransactionResponse, error) {
	if r.rst != nil {
		return r.rst, nil
	}
	client := pb2.NewTransactionStreamServiceClient(r.node.PrivConn())
	stream, err := client.Stream(ctx, &pb2.TransactionStreamRequest{
		Txid: [][]byte{r.txid},
	})
	if err != nil {
		return nil, err
	}
	defer stream.CloseSend()
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
