package validation

import (
	"context"
	"fmt"
	"sync"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/systest/cluster"
)

type ConsensusData struct {
	Consensus, State []byte
}

func getConsensusData(ctx context.Context, distance int, node *cluster.NodeClient) *ConsensusData {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	meshapi := pb.NewMeshServiceClient(node.PubConn())
	lid, err := meshapi.CurrentLayer(ctx, &pb.CurrentLayerRequest{})
	if err != nil {
		return nil
	}
	layers, err := meshapi.EpochNumLayers(ctx, &pb.EpochNumLayersRequest{})
	if err != nil {
		return nil
	}
	target := int(lid.Layernum.Number) - distance
	if target < 2*int(layers.Numlayers.Number) {
		// empty strings are always in consensus
		return &ConsensusData{}
	}
	ls, err := meshapi.LayersQuery(ctx, &pb.LayersQueryRequest{
		StartLayer: &pb.LayerNumber{Number: uint32(target)},
		EndLayer:   &pb.LayerNumber{Number: uint32(target)},
	})
	if err != nil {
		return nil
	}
	if len(ls.Layer) != 1 {
		return nil
	}
	layer := ls.Layer[0]
	return &ConsensusData{Consensus: layer.Hash, State: layer.RootStateHash}
}

// failMinority should increment number of failures for groups smaller than the largest one
// if there are several groups of the same size they all should be considered as failed.
func failMinority(failures []int, groups map[string][]int) {
	var (
		largest  []int
		sameSize int
	)
	for _, group := range groups {
		failed := group
		if len(group) > len(largest) {
			failed = largest
			largest = group
			sameSize = 0
		} else if len(group) == len(largest) {
			sameSize++
		}
		for _, id := range failed {
			failures[id]++
		}
	}
	if sameSize > 0 {
		for _, id := range largest {
			failures[id]++
		}
	}
}

func Consensus(c *cluster.Cluster, tolerate, distance int) Validation {
	cv := NewConsensusValidation(c.Total(), tolerate)
	return func(ctx context.Context) error {
		var (
			eg   errgroup.Group
			iter = cv.Next()
		)
		for i := range c.Total() {
			node := c.Client(i)
			eg.Go(func() error {
				iter.OnData(i, getConsensusData(ctx, distance, node))
				return nil
			})
		}
		eg.Wait()
		return cv.Complete(iter)
	}
}

func NewConsensusValidation(size, tolerate int) *ConsensusValidation {
	return &ConsensusValidation{failures: make([]int, size), tolerate: tolerate}
}

type ConsensusValidation struct {
	failures []int
	tolerate int
}

func (c *ConsensusValidation) Next() *ConsensusValidationIteration {
	return &ConsensusValidationIteration{
		all:       make([]*ConsensusData, len(c.failures)),
		consensus: map[string][]int{},
		state:     map[string][]int{},
	}
}

func (c *ConsensusValidation) Complete(iter *ConsensusValidationIteration) error {
	prev := make([]int, len(c.failures))
	copy(prev, c.failures)
	for i, data := range iter.all {
		if data == nil {
			c.failures[i]++
		}
	}
	if len(iter.consensus) > 1 {
		failMinority(c.failures, iter.consensus)
	} else if len(iter.state) > 1 {
		failMinority(c.failures, iter.state)
	}
	for i, n := range c.failures {
		if n == prev[i] {
			c.failures[i] = 0
		}
	}
	for i, rst := range c.failures {
		if rst > c.tolerate {
			return fmt.Errorf("node %d failed to reach consensus in %d period(s)",
				i, rst,
			)
		}
	}
	return nil
}

type ConsensusValidationIteration struct {
	mu        sync.Mutex
	all       []*ConsensusData
	consensus map[string][]int
	state     map[string][]int
}

func (iter *ConsensusValidationIteration) OnData(id int, data *ConsensusData) {
	if data == nil {
		return
	}
	iter.mu.Lock()
	defer iter.mu.Unlock()

	iter.all[id] = data
	iter.consensus[string(data.Consensus)] = append(
		iter.consensus[string(data.Consensus)],
		id,
	)
	iter.state[string(data.State)] = append(
		iter.state[string(data.State)],
		id,
	)
}
