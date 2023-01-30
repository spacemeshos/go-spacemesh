package validation

import (
	"context"
	"fmt"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/systest/cluster"
)

// Periodic runs one validation immediately and runs it every once in a period.
func Periodic(ctx context.Context, period time.Duration, f Validation) error {
	if err := f(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(period)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := f(ctx); err != nil {
				return err
			}
		}
	}
}

func isSynced(ctx context.Context, node *cluster.NodeClient) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	svc := pb.NewNodeServiceClient(node)
	resp, err := svc.Status(ctx, &pb.StatusRequest{})
	if err != nil {
		return false
	}
	return resp.Status.IsSynced
}

type Validation func(context.Context) error

func Sync(c *cluster.Cluster, tolerate int) Validation {
	failures := make([]int, c.Total())
	return func(ctx context.Context) error {
		var eg errgroup.Group
		for i := 0; i < c.Total(); i++ {
			i := i
			node := c.Client(i)
			eg.Go(func() error {
				if !isSynced(ctx, node) {
					failures[i]++
				} else {
					failures[i] = 0
				}
				return nil
			})
		}
		eg.Wait()
		for i, rst := range failures {
			if rst > tolerate {
				return fmt.Errorf("node %s wasn't able to sync in %d periods",
					c.Client(i).Name, rst,
				)
			}
		}
		return nil
	}
}
