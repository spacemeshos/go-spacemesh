package grpcserver

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strconv"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/spacemeshos/go-spacemesh/common/fixture"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

func TestTransactionService_StreamResults(t *testing.T) {
	db := sql.InMemory()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	gen := fixture.NewTransactionResultGenerator().
		WithAddresses(2)
	txs := make([]types.TransactionWithResult, 100)
	require.NoError(t, db.WithTx(ctx, func(dtx *sql.Tx) error {
		for i := range txs {
			tx := gen.Next()

			require.NoError(t, transactions.Add(dtx, &tx.Transaction, time.Time{}))
			require.NoError(t, transactions.AddResult(dtx, tx.ID, &tx.TransactionResult))
			txs[i] = *tx
		}
		return nil
	}))

	svc := NewTransactionService(db, nil, nil, nil, nil)
	t.Cleanup(launchServer(t, svc))

	conn, err := grpc.DialContext(ctx,
		"localhost:"+strconv.Itoa(cfg.GrpcServerPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	client := pb.NewTransactionServiceClient(conn)

	t.Run("All", func(t *testing.T) {
		stream, err := client.StreamResults(ctx, &pb.TransactionResultsRequest{})
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
					Address: gen.Addrs[0].String(),
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

func BenchmarkStreamResults(b *testing.B) {
	db := sql.InMemory()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var (
		gen      = fixture.NewTransactionResultGenerator().WithAddresses(10_000)
		count    = map[types.Address]int{}
		maxaddr  types.Address
		maxcount int
		start    = time.Now()
	)
	tx, err := db.Tx(ctx)
	require.NoError(b, err)
	for i := 0; i < 1_000; i++ {
		rst := gen.Next()
		for _, addr := range rst.Addresses {
			count[addr]++
			if count[addr] > maxcount {
				maxaddr = addr
				maxcount = count[addr]
			}
		}
		require.NoError(b, transactions.Add(tx, &rst.Transaction, time.Time{}))
		require.NoError(b, transactions.AddResult(tx, rst.ID, &rst.TransactionResult))
	}
	require.NoError(b, tx.Commit())
	require.NoError(b, tx.Release())
	svc := NewTransactionService(db, nil, nil, nil, nil)
	b.Cleanup(launchServer(b, svc))

	conn, err := grpc.DialContext(ctx,
		"localhost:"+strconv.Itoa(cfg.GrpcServerPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	require.NoError(b, err)
	b.Cleanup(func() { require.NoError(b, conn.Close()) })
	client := pb.NewTransactionServiceClient(conn)

	b.Logf("setup took %s", time.Since(start))
	b.ResetTimer()
	b.ReportAllocs()

	stats := runtime.MemStats{}
	for i := 0; i < b.N; i++ {
		stream, err := client.StreamResults(ctx, &pb.TransactionResultsRequest{Address: maxaddr.String()})
		if err != nil {
			b.Fatal(err)
		}
		n := 0
		for {
			_, err := stream.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				b.Fatal(err)
			}
			n++
		}

		runtime.ReadMemStats(&stats)
		b.Logf("memory requested %v", stats.Sys)
		require.Equal(b, maxcount, n)
	}
}
