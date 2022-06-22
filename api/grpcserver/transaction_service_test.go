package grpcserver

import (
	"context"
	"errors"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/fixture"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestTransactionService_StreamResults(t *testing.T) {
	db := sql.InMemory()

	gen := fixture.NewTransactionResultGenerator().
		WithAddresses(2)
	txs := make([]types.TransactionWithResult, 100)
	for i := range txs {
		tx := gen.Next()

		require.NoError(t, transactions.Add(db, &tx.Transaction, time.Time{}))
		require.NoError(t, transactions.AddResult(db, tx.ID, &tx.TransactionResult))
		txs[i] = *tx
	}

	svc := NewTransactionService(db, nil, nil, nil, nil)
	t.Cleanup(launchServer(t, svc))

	conn, err := grpc.Dial("localhost:"+strconv.Itoa(cfg.GrpcServerPort),
		grpc.WithInsecure())
	require.NoError(t, err)
	client := pb.NewTransactionServiceClient(conn)

	t.Run("All", func(t *testing.T) {
		stream, err := client.StreamResults(context.Background(),
			&pb.TransactionResultsRequest{})
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
	})
	t.Run("Watch", func(t *testing.T) {
		events.InitializeReporter()
		t.Cleanup(events.CloseEventReporter)

		const (
			start = 100
			n     = 10
		)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stream, err := client.StreamResults(ctx,
			&pb.TransactionResultsRequest{Start: start, Watch: true})
		require.NoError(t, err)
		_, err = stream.Header()
		require.NoError(t, err)
		gen = gen.WithLayers(start, 10)
		for i := 0; i < n; i++ {
			events.ReportResult(*gen.Next())
		}
		for i := 0; i < n; i++ {
			received, err := stream.Recv()
			require.NoError(t, err)
			require.GreaterOrEqual(t, received.Layer, uint32(start))
		}
	})
}
