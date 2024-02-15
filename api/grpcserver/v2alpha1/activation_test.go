package v2alpha1

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/fixture"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

func TestActivationService_List(t *testing.T) {
	db := sql.InMemory()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	gen := fixture.NewAtxsGenerator()
	activations := make([]types.VerifiedActivationTx, 100)
	require.NoError(t, db.WithTx(ctx, func(dtx *sql.Tx) error {
		for i := range activations {
			atx := gen.Next()
			require.NoError(t, atxs.Add(dtx, atx))
			activations[i] = *atx
		}
		return nil
	}))

	svc := NewActivationService(db)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(ctx, t, cfg)
	client := spacemeshv2alpha1.NewActivationServiceClient(conn)

	t.Run("limit set too high", func(t *testing.T) {
		_, err := client.List(ctx, &spacemeshv2alpha1.ActivationRequest{Limit: 200})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		require.Equal(t, s.Message(), "limit is capped at 100")
	})

	t.Run("no limit set", func(t *testing.T) {
		_, err := client.List(ctx, &spacemeshv2alpha1.ActivationRequest{})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		require.Equal(t, s.Message(), "limit must be set to <= 100")
	})

	t.Run("limit and offset", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.ActivationRequest{
			Limit:  25,
			Offset: 50,
		})
		require.NoError(t, err)
		require.Equal(t, 25, len(list.Activations))
	})

	t.Run("all", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.ActivationRequest{Limit: 100})
		require.NoError(t, err)
		require.Equal(t, len(activations), len(list.Activations))
	})

	t.Run("coinbase", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.ActivationRequest{
			Limit:    1,
			Coinbase: activations[3].Coinbase.String(),
		})
		require.NoError(t, err)
		require.Equal(t, activations[3].ID().Bytes(), list.GetActivations()[0].GetV1().GetId())
	})

	t.Run("nodeId", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.ActivationRequest{
			Limit:  1,
			NodeId: activations[1].SmesherID.Bytes(),
		})
		require.NoError(t, err)
		require.Equal(t, activations[1].ID().Bytes(), list.GetActivations()[0].GetV1().GetId())
	})

	t.Run("id", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.ActivationRequest{
			Limit: 1,
			Id:    activations[3].ID().Bytes(),
		})
		require.NoError(t, err)
		require.Equal(t, activations[3].ID().Bytes(), list.GetActivations()[0].GetV1().GetId())
	})
}

func TestActivationStreamService_Stream(t *testing.T) {
	db := sql.InMemory()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	gen := fixture.NewAtxsGenerator()
	activations := make([]types.VerifiedActivationTx, 100)
	require.NoError(t, db.WithTx(ctx, func(dtx *sql.Tx) error {
		for i := range activations {
			atx := gen.Next()
			require.NoError(t, atxs.Add(dtx, atx))
			activations[i] = *atx
		}
		return nil
	}))

	svc := NewActivationStreamService(db)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(ctx, t, cfg)
	client := spacemeshv2alpha1.NewActivationStreamServiceClient(conn)

	t.Run("all", func(t *testing.T) {
		events.InitializeReporter()
		t.Cleanup(events.CloseEventReporter)

		stream, err := client.Stream(ctx, &spacemeshv2alpha1.ActivationStreamRequest{})
		require.NoError(t, err)

		var i int
		for {
			_, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			i++
		}
		require.Equal(t, len(activations), i)
	})

	t.Run("watch", func(t *testing.T) {
		events.InitializeReporter()
		t.Cleanup(events.CloseEventReporter)

		const (
			start = 100
			n     = 10
		)

		gen = fixture.NewAtxsGenerator().WithEpochs(start, 10)
		var streamed []*events.ActivationTx
		for i := 0; i < n; i++ {
			streamed = append(streamed, &events.ActivationTx{VerifiedActivationTx: gen.Next()})
		}

		for _, tc := range []struct {
			desc    string
			request *spacemeshv2alpha1.ActivationStreamRequest
		}{
			{
				desc: "ID",
				request: &spacemeshv2alpha1.ActivationStreamRequest{
					Id:         streamed[3].ID().Bytes(),
					StartEpoch: start,
					Watch:      true,
				},
			},
			{
				desc: "NodeID",
				request: &spacemeshv2alpha1.ActivationStreamRequest{
					NodeId:     streamed[3].NodeID.Bytes(),
					StartEpoch: start,
					Watch:      true,
				},
			},
			{
				desc: "Coinbase",
				request: &spacemeshv2alpha1.ActivationStreamRequest{
					Coinbase:   streamed[3].Coinbase.String(),
					StartEpoch: start,
					Watch:      true,
				},
			},
		} {
			tc := tc
			t.Run(tc.desc, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				stream, err := client.Stream(ctx, tc.request)
				require.NoError(t, err)
				_, err = stream.Header()
				require.NoError(t, err)

				var expect []*types.VerifiedActivationTx
				for _, rst := range streamed {
					events.ReportNewActivation(rst.VerifiedActivationTx)
					matcher := resultsMatcher{tc.request, ctx}
					if matcher.match(rst) {
						expect = append(expect, rst.VerifiedActivationTx)
					}
				}

				for _, rst := range expect {
					received, err := stream.Recv()
					require.NoError(t, err)
					require.Equal(t, toAtx(rst).String(), received.GetV1().String())
				}
			})
		}
	})
}

func TestActivationService_ActivationsCount(t *testing.T) {
	db := sql.InMemory()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	gen := fixture.NewAtxsGenerator().WithEpochs(0, 1)
	activations := make([]types.VerifiedActivationTx, 30)
	require.NoError(t, db.WithTx(ctx, func(dtx *sql.Tx) error {
		for i := range activations {
			atx := gen.Next()
			require.NoError(t, atxs.Add(dtx, atx))
			activations[i] = *atx
		}
		return nil
	}))

	svc := NewActivationService(db)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(ctx, t, cfg)
	client := spacemeshv2alpha1.NewActivationServiceClient(conn)

	count, err := client.ActivationsCount(ctx, &spacemeshv2alpha1.ActivationsCountRequest{
		Epoch: activations[3].PublishEpoch.Uint32(),
	})
	require.NoError(t, err)
	require.Equal(t, len(activations), int(count.Count))
}
