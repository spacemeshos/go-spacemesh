package validation

import (
	"context"
	"fmt"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/systest/cluster"
)

// Periodic runs validation once in a period, starting immediately.
func Periodic(ctx context.Context, period time.Duration, f Validation) error {
	if err := f(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(period)
	defer ticker.Stop()
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

type Validation func(context.Context) error

func isSynced(ctx context.Context, node *cluster.NodeClient) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	svc := pb.NewNodeServiceClient(node.PubConn())
	resp, err := svc.Status(ctx, &pb.StatusRequest{})
	if err != nil {
		return false
	}
	return resp.Status.IsSynced
}

func Sync(c *cluster.Cluster, tolerate int) Validation {
	sv := &SyncValidation{
		failures: make([]int, c.Total()),
		tolerate: tolerate,
	}
	return func(ctx context.Context) error {
		var eg errgroup.Group
		for i := range c.Total() {
			node := c.Client(i)
			eg.Go(func() error {
				return sv.OnData(i, isSynced(ctx, node))
			})
		}
		return eg.Wait()
	}
}

type SyncValidation struct {
	failures []int
	tolerate int
}

func (s *SyncValidation) OnData(id int, synced bool) error {
	if !synced {
		s.failures[id]++
		if rst := s.failures[id]; rst > s.tolerate {
			return fmt.Errorf("node %d not synced in %d periods",
				id, rst,
			)
		}
	} else {
		s.failures[id] = 0
	}
	return nil
}
