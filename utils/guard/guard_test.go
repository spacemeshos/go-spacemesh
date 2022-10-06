package guard_test

import (
	"context"
	"testing"

	"github.com/spacemeshos/go-spacemesh/utils/guard"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestApply(t *testing.T) {
	r := require.New(t)
	g := guard.New(0)

	g.Apply(func(t *int) {
		r.NotNil(t)
		r.Equal(*t, 0)
	})

	g.Apply(func(t *int) {
		*t = 123
	})

	g.Apply(func(t *int) {
		r.Equal(*t, 123)
	})
}

func TestRace(t *testing.T) {
	r := require.New(t)
	g := guard.New(0)

	eg, _ := errgroup.WithContext(context.TODO())
	for i := 0; i < 10; i++ {
		eg.Go(func() error {
			for iter := 0; iter < 100; iter++ {
				g.Apply(func(t *int) {
					*t += 1
				})
			}
			return nil
		})
	}
	eg.Wait()

	g.Apply(func(t *int) {
		r.Equal(*t, 1000)
	})
}
