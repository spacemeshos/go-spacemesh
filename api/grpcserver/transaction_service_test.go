package grpcserver

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestTransactionService_StreamResults(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(101))

	addrs := make([]types.Address, 10)
	for i := range addrs {
		addrs[i] = types.Address{byte(i)}
	}
	blocks := make([]types.BlockID, 10)
	for i := range blocks {
		blocks[i] = types.BlockID{byte(i)}
	}
	layers := make([]types.LayerID, 10)
	for i := range layers {
		layers[i] = types.NewLayerID(uint32(1 + i))
	}
	txs := make([]types.TransactionWithResult, 10000)
	for i := range txs {
		rng.Read(txs[i].ID[:])
		txs[i].Raw = make([]byte, 10)
		rng.Read(txs[i].Raw)
		txs[i].Block = blocks[rng.Intn(len(blocks))]
		txs[i].Layer = layers[rng.Intn(len(layers))]
		if lth := rng.Intn(len(addrs)); lth > 0 {
			txs[i].Addresses = make([]types.Address, lth)

			rng.Shuffle(len(addrs), func(i, j int) {
				addrs[i], addrs[j] = addrs[j], addrs[i]
			})
			copy(txs[i].Addresses, addrs)
		}

		require.NoError(t, transactions.Add(db, &txs[i].Transaction, time.Time{}))
		require.NoError(t, transactions.AddResult(db, txs[i].ID, &txs[i].TransactionResult))
	}

	svc := NewTransactionService(db, nil, nil, nil, nil)
	closer := launchServer(t, svc)
	t.Cleanup(closer)

	conn, err := grpc.Dial("localhost:"+strconv.Itoa(cfg.GrpcServerPort),
		grpc.WithInsecure())
	require.NoError(t, err)
	client := pb.NewTransactionServiceClient(conn)
	stream, err := client.StreamResults(context.Background(), &pb.TransactionResultsRequest{})
	require.NoError(t, err)
	var i int
	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		i++
	}
	require.Equal(t, len(txs), i)
}
