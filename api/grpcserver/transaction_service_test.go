package grpcserver

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"runtime"
	"testing"
	"time"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/fixture"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	"github.com/spacemeshos/go-spacemesh/txs"
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

	svc := NewTransactionService(db, nil, nil, nil, nil, nil)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(ctx, t, cfg)
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
		for range n {
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
	for range 1_000 {
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
	svc := NewTransactionService(db, nil, nil, nil, nil, nil)
	cfg, cleanup := launchServer(b, svc)
	b.Cleanup(cleanup)

	conn := dialGrpc(ctx, b, cfg)
	client := pb.NewTransactionServiceClient(conn)

	b.Logf("setup took %s", time.Since(start))
	b.ResetTimer()
	b.ReportAllocs()

	stats := runtime.MemStats{}
	for range b.N {
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

type parseExpectation func(tb testing.TB, resp *pb.ParseTransactionResponse, err error)

func expectParseError(code codes.Code, message string) parseExpectation {
	return func(tb testing.TB, resp *pb.ParseTransactionResponse, err error) {
		s, ok := status.FromError(err)
		require.True(tb, ok)
		assert.Equal(tb, code, s.Code())
		assert.Contains(tb, s.Message(), message)
	}
}

func parseOk() parseExpectation {
	return func(tb testing.TB, resp *pb.ParseTransactionResponse, err error) {
		require.NoError(tb, err)
		require.NotEmpty(tb, resp)
	}
}

func TestParseTransactions(t *testing.T) {
	db := sql.InMemory()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	vminst := vm.New(db)
	cfg, cleanup := launchServer(t, NewTransactionService(db, nil, nil, txs.NewConservativeState(vminst, db), nil, nil))
	t.Cleanup(cleanup)
	var (
		conn     = dialGrpc(ctx, t, cfg)
		client   = pb.NewTransactionServiceClient(conn)
		keys     = make([]signing.PrivateKey, 4)
		accounts = make([]types.Account, len(keys))
		rng      = rand.New(rand.NewSource(10101))
	)
	for i := range keys {
		pub, priv, err := ed25519.GenerateKey(rng)
		require.NoError(t, err)
		keys[i] = signing.PrivateKey(priv)
		accounts[i] = types.Account{Address: wallet.Address(pub), Balance: 1e12}
	}
	require.NoError(t, vminst.ApplyGenesis(accounts))
	_, _, err := vminst.Apply(vm.ApplyContext{Layer: types.GetEffectiveGenesis().Add(1)},
		[]types.Transaction{{RawTx: types.NewRawTx(wallet.SelfSpawn(keys[0], 0))}}, nil)
	require.NoError(t, err)
	mangled := wallet.Spend(keys[0], accounts[3].Address, 100, 0)
	mangled[len(mangled)-1] -= 1

	for _, tc := range []struct {
		desc   string
		tx     []byte
		verify bool
		expect parseExpectation
	}{
		{
			"empty",
			nil,
			false,
			expectParseError(codes.InvalidArgument, "empty"),
		},
		{
			"malformed",
			[]byte("something"),
			false,
			expectParseError(codes.InvalidArgument, "malformed tx"),
		},
		{
			"not spawned",
			wallet.Spend(keys[2], accounts[3].Address, 100, 0),
			false,
			expectParseError(codes.NotFound, "not spawned"),
		},
		{
			"all good",
			wallet.Spend(keys[0], accounts[3].Address, 100, 0),
			false,
			parseOk(),
		},
		{
			"all good with verification",
			wallet.Spend(keys[0], accounts[3].Address, 100, 0),
			true,
			parseOk(),
		},
		{
			"mangled signature",
			mangled,
			true,
			expectParseError(codes.InvalidArgument, "signature is invalid"),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			resp, err := client.ParseTransaction(context.Background(), &pb.ParseTransactionRequest{
				Transaction: tc.tx,
				Verify:      tc.verify,
			})
			tc.expect(t, resp, err)
		})
	}
}
