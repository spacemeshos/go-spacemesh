package v2alpha1

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"testing"
	"time"

	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

func TestLayerService_List(t *testing.T) {
	db := sql.InMemory()
	ctx := context.Background()

	lrs := make([]layers.Layer, 100)
	r1 := rand.New(rand.NewSource(time.Now().UnixNano()))
	r2 := rand.New(rand.NewSource(time.Now().UnixNano() + 1))
	for i := range lrs {
		processed := r1.Intn(2) == 0
		withBlock := r2.Intn(2) == 0
		l, err := generateLayer(db, types.LayerID(i), layerGenProcessed(processed), layerGenWithBlock(withBlock))
		require.NoError(t, err)
		lrs[i] = *l
	}

	svc := NewLayerService(db)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(ctx, t, cfg)
	client := spacemeshv2alpha1.NewLayerServiceClient(conn)

	t.Run("limit set too high", func(t *testing.T) {
		_, err := client.List(ctx, &spacemeshv2alpha1.LayerRequest{Limit: 200})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		require.Equal(t, "limit is capped at 100", s.Message())
	})

	t.Run("no limit set", func(t *testing.T) {
		_, err := client.List(ctx, &spacemeshv2alpha1.LayerRequest{})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		require.Equal(t, "limit must be set to <= 100", s.Message())
	})

	t.Run("limit and offset", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.LayerRequest{
			Limit:  25,
			Offset: 50,
		})
		require.NoError(t, err)
		require.Len(t, list.Layers, 25)
	})

	t.Run("all", func(t *testing.T) {
		ls, err := client.List(ctx, &spacemeshv2alpha1.LayerRequest{
			StartLayer: 0,
			EndLayer:   100,
			Limit:      100,
		})
		require.NoError(t, err)
		require.Len(t, lrs, len(ls.Layers))
	})
}

func TestLayerConvertEventStatus(t *testing.T) {
	s := convertEventStatus(events.LayerStatusTypeApproved)
	require.Equal(t, spacemeshv2alpha1.Layer_LAYER_STATUS_APPLIED, s)

	s = convertEventStatus(events.LayerStatusTypeConfirmed)
	require.Equal(t, spacemeshv2alpha1.Layer_LAYER_STATUS_VERIFIED, s)

	s = convertEventStatus(events.LayerStatusTypeApplied)
	require.Equal(t, spacemeshv2alpha1.Layer_LAYER_STATUS_VERIFIED, s)

	s = convertEventStatus(events.LayerStatusTypeUnknown)
	require.Equal(t, spacemeshv2alpha1.Layer_LAYER_STATUS_UNSPECIFIED, s)
}

func TestLayerStreamService_Stream(t *testing.T) {
	db := sql.InMemory()
	ctx := context.Background()

	lrs := make([]layers.Layer, 100)
	r1 := rand.New(rand.NewSource(time.Now().UnixNano()))
	r2 := rand.New(rand.NewSource(time.Now().UnixNano() + 1))
	for i := range lrs {
		processed := r1.Intn(2) == 0
		withBlock := r2.Intn(2) == 0
		l, err := generateLayer(db, types.LayerID(i), layerGenProcessed(processed), layerGenWithBlock(withBlock))
		require.NoError(t, err)
		lrs[i] = *l
	}

	svc := NewLayerStreamService(db)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(ctx, t, cfg)
	client := spacemeshv2alpha1.NewLayerStreamServiceClient(conn)

	t.Run("all", func(t *testing.T) {
		events.InitializeReporter()
		t.Cleanup(events.CloseEventReporter)

		stream, err := client.Stream(ctx, &spacemeshv2alpha1.LayerStreamRequest{})
		require.NoError(t, err)

		var i int
		for {
			l, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			assert.Equal(t, toLayer(&lrs[i]).String(), l.String())
			i++
		}
		require.Len(t, lrs, i)
	})

	t.Run("watch", func(t *testing.T) {
		events.InitializeReporter()
		t.Cleanup(events.CloseEventReporter)

		const (
			start = 100
			n     = 10
		)

		var streamed []layers.Layer
		for i := start + 0; i < start+n; i++ {
			layer, err := generateLayer(db, types.LayerID(i), layerGenProcessed(true), layerGenWithBlock(true))
			require.NoError(t, err)
			streamed = append(streamed, *layer)
		}

		for _, tc := range []struct {
			desc    string
			request *spacemeshv2alpha1.LayerStreamRequest
		}{
			{
				desc: "layers",
				request: &spacemeshv2alpha1.LayerStreamRequest{
					StartLayer: start,
					EndLayer:   start,
					Watch:      true,
				},
			},
		} {
			t.Run(tc.desc, func(t *testing.T) {
				stream, err := client.Stream(ctx, tc.request)
				require.NoError(t, err)
				_, err = stream.Header()
				require.NoError(t, err)

				var expect []*layers.Layer
				for _, rst := range streamed {
					s := events.LayerStatusTypeUnknown
					if !rst.AppliedBlock.IsEmpty() {
						s = events.LayerStatusTypeApproved
					}

					if rst.Processed {
						s = events.LayerStatusTypeConfirmed
					}

					lu := events.LayerUpdate{
						LayerID: rst.Id,
						Status:  s,
					}

					events.ReportLayerUpdate(lu)
					matcher := layersMatcher{tc.request, ctx}
					if matcher.match(&lu) {
						expect = append(expect, &rst)
					}
				}

				for _, rst := range expect {
					received, err := stream.Recv()
					require.NoError(t, err)
					require.Equal(t, toLayer(rst).String(), received.String())
				}
			})
		}
	})
}

type layerGenOpt = func(o *layerGenOpts)

type layerGenOpts struct {
	processed bool
	withBlock bool
}

func layerGenProcessed(processed bool) layerGenOpt {
	return func(g *layerGenOpts) {
		g.processed = processed
	}
}

func layerGenWithBlock(withBlock bool) layerGenOpt {
	return func(g *layerGenOpts) {
		g.withBlock = withBlock
	}
}

func generateLayer(db *sql.Database, id types.LayerID, opts ...layerGenOpt) (*layers.Layer, error) {
	g := &layerGenOpts{}
	for _, opt := range opts {
		opt(g)
	}

	var block *types.Block = nil

	if g.withBlock {
		block = &types.Block{
			InnerBlock: types.InnerBlock{
				LayerIndex: id,
				TxIDs:      nil,
			},
		}
		block.Initialize()

		err := blocks.Add(db, block)
		if err != nil {
			return nil, err
		}

		err = layers.SetApplied(db, id, block.ID())
		if err != nil {
			return nil, err
		}
	}

	if g.processed {
		err := layers.SetProcessed(db, id)
		if err != nil {
			return nil, err
		}
	}

	meshHash := types.RandomHash()
	err := layers.SetMeshHash(db, id, meshHash)
	if err != nil {
		return nil, err
	}

	stateHash := types.RandomHash()
	err = layers.UpdateStateHash(db, id, stateHash)
	if err != nil {
		return nil, err
	}

	l := &layers.Layer{
		Id:             id,
		Processed:      g.processed,
		StateHash:      stateHash,
		AggregatedHash: meshHash,
	}

	if block != nil {
		l.AppliedBlock = block.ID()
		l.Block = block
	}

	return l, nil
}
