package validation

import (
	"container/list"
	"context"
	"errors"
	"fmt"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

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

type state struct {
	name string
	list.List
}

type event struct {
	identity  int
	layer     int
	consensus []byte
	state     []byte
}

func eventsCollector(ctx context.Context, consumer chan<- event, identity int) layerCollector {
	return func(layer *pb.LayerStreamResponse) (bool, error) {
		if layer.Layer.Status != pb.Layer_LAYER_STATUS_APPLIED {
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case consumer <- event{
			identity:  identity,
			layer:     int(layer.Layer.Number.Number),
			consensus: layer.Layer.Hash,
			state:     layer.Layer.RootStateHash,
		}:
		}
		return true, nil
	}
}

func eventsWatcher(ctx context.Context, tctx *testcontext.Context, client *cluster.NodeClient, collector layerCollector) func() error {
	return func() error {
		for {
			// TODO double check that connection backoff is enabled and works as assumed
			// otherwise this will spin
			err := layersStream(ctx, client, collector)
			if errors.Is(err, context.Canceled) {
				return err
			}
			if err != nil {
				tctx.Log.Debugw("temporary error in consuming layers",
					"name", client.Name,
					"error", err.Error(),
				)
			}
		}
	}
}

func ensureConsistency(states []state) error {
	var (
		expectedLayer        *int
		consensusConsistency = map[string][]string{}
		stateConsistency     = map[string][]string{}
	)
	for i, state := range states {
		if state.Len() == 0 {
			return fmt.Errorf("node %s has no applied layers", state.name)
		}
		e := state.Back().Value.(*event)
		if expectedLayer == nil {
			expectedLayer = &e.layer
		} else if *expectedLayer != e.layer {
			return fmt.Errorf("node %d/%s stores corrupted state", i, state.name)
		}
		consensusConsistency[string(e.consensus)] = append(consensusConsistency[string(e.consensus)], state.name)
		stateConsistency[string(e.state)] = append(stateConsistency[string(e.state)], state.name)
	}
	if len(consensusConsistency) > 1 {
		msg := "consensus fork:"
		for hash, nodes := range consensusConsistency {
			msg += fmt.Sprintf(" %+v=0x%x", nodes, []byte(hash))
		}
		return errors.New(msg)
	}
	if len(stateConsistency) > 1 {
		msg := "state fork:"
		for hash, nodes := range stateConsistency {
			msg += fmt.Sprintf(" %+v=0x%x", nodes, []byte(hash))
		}
		return errors.New(msg)
	}
	return nil
}

func validate(states []state, distance int) error {
	consistent := false
	for _, state := range states {
		if state.Len() == 0 {
			continue
		}
		front := state.Front().Value.(*event)
		back := state.Back().Value.(*event)
		if !consistent && front.layer-back.layer == distance {
			if err := ensureConsistency(states); err != nil {
				return err
			}
			consistent = true
		}
	}
	for i := range states {
		if back := states[i].Back(); consistent && back != nil {
			states[i].Remove(back)
		}
	}
	return nil
}

func RunConsensusValidation(ctx context.Context, tctx *testcontext.Context, c *cluster.Cluster, distance int) error {
	consumer := make(chan event, c.Total())
	eg, ctx := errgroup.WithContext(ctx)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for i := 0; i < c.Total(); i++ {
		eg.Go(eventsWatcher(ctx, tctx, c.Client(i), eventsCollector(ctx, consumer, i)))
	}
	go func() {
		eg.Wait()
		close(consumer)
	}()
	var (
		results = make([]state, c.Total())
		err     error
	)
	for i := range results {
		results[i].name = c.Client(i).Name
	}
	for e := range consumer {
		rst := &results[e.identity]
		if rst.Len() != 0 {
			prev := rst.Front().Value.(*event)
			if prev.layer+1 != e.layer {
				err = fmt.Errorf("node %s received events out of order for layers %d and %d", c.Client(e.identity).Name, prev.layer, e.layer)
				break
			}
		}
		rst.PushFront(e)
		err = validate(results, distance)
		if err != nil {
			break
		}
	}
	cancel()
	werr := eg.Wait()
	if err == nil && !errors.Is(werr, context.Canceled) {
		return werr
	}
	return err
}
