package grpcserver

import (
	"context"
	"errors"
	"io"
	"strconv"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/spacemeshos/go-spacemesh/common/fixture"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
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
		gen = fixture.NewTransactionResultGenerator().
			WithAddresses(2).WithLayers(start, 10)
		var streamed []*types.TransactionWithResult
		for i := 0; i < n; i++ {
			streamed = append(streamed, gen.Next())
		}

		for _, tc := range []struct {
			desc    string
			matcher resultsMatcher
			request *pb.TransactionResultsRequest
		}{
			{
				desc:    "ID",
				matcher: resultsMatcher{TID: &streamed[3].ID},
				request: &pb.TransactionResultsRequest{
					Id:    streamed[3].ID[:],
					Start: start,
					Watch: true,
				},
			},
			{
				desc:    "Address",
				matcher: resultsMatcher{Address: &gen.Addrs[0]},
				request: &pb.TransactionResultsRequest{
					Address: gen.Addrs[0][:],
					Start:   start,
					Watch:   true,
				},
			},
		} {
			tc := tc
			t.Run(tc.desc, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				stream, err := client.StreamResults(ctx, tc.request)
				require.NoError(t, err)
				_, err = stream.Header()
				require.NoError(t, err)

				var expect []*types.TransactionWithResult
				for _, rst := range streamed {
					events.ReportResult(*rst)
					if tc.matcher.match(rst) {
						expect = append(expect, rst)
					}
				}
				for _, rst := range expect {
					received, err := stream.Recv()
					require.NoError(t, err)
					require.Equal(t, castResult(rst).String(), received.String(),
						"0x%x != %s", received.Tx.Id, rst.ID.String())
				}
			})
		}
	})
}
