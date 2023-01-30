package validation

import (
	"context"
	"fmt"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/systest/cluster"
)

type consensusData struct {
	consensus, state []byte
}

func getConsensusData(ctx context.Context, distance int, node *cluster.NodeClient) *consensusData {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	meshapi := pb.NewMeshServiceClient(node)
	lid, err := meshapi.CurrentLayer(ctx, &pb.CurrentLayerRequest{})
	if err != nil {
		return nil
	}
	layers, err := meshapi.EpochNumLayers(ctx, &pb.EpochNumLayersRequest{})
	if err != nil {
		return nil
	}
	target := int(lid.Layernum.Number) - distance
	if target < 2*int(layers.Numlayers.Value) {
		// empty strings are always in consensus
		return &consensusData{}
	}
	meshapi.LayersQuery(ctx, &pb.LayersQueryRequest{
		StartLayer: &pb.LayerNumber{Number: uint32(target)},
		EndLayer:   &pb.LayerNumber{Number: uint32(target)},
	})
	return nil

}

// failMinority should increment number of failures for groups smaller than the largest one
// if there are several groups of the same largest size they all should be considered as failed
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
	failures := make([]int, c.Total())
	return func(ctx context.Context) error {
		var (
			eg      errgroup.Group
			results = make([]*consensusData, c.Total())
		)
		for i := 0; i < c.Total(); i++ {
			i := i
			node := c.Client(i)
			eg.Go(func() error {
				results[i] = getConsensusData(ctx, distance, node)
				return nil
			})
		}
		eg.Wait()
		var (
			consensus = map[string][]int{}
			state     = map[string][]int{}
		)
		for i, data := range results {
			if data == nil {
				failures[i]++
				continue
			}
			consensus[string(data.consensus)] = append(consensus[string(data.consensus)], i)
			state[string(data.state)] = append(state[string(data.state)], i)
		}
		if len(consensus) > 1 {
			failMinority(failures, consensus)
		} else if len(state) > 1 {
			failMinority(failures, state)
		}

		for i, rst := range failures {
			if rst > tolerate {
				return fmt.Errorf("node %s wasn't able to recover consensus consistency in %d periods",
					c.Client(i).Name, rst,
				)
			}
		}
		return nil
	}
}
