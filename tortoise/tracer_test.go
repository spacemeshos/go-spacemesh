package tortoise

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/tortoise/sim"
)

func TestTracer(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "tortoise.trace")
	const size = 12
	s := sim.New(
		sim.WithLayerSize(size),
	)
	s.Setup()

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.WindowSize = 10
	trt := tortoiseFromSimState(t, s.GetState(0), WithConfig(cfg), WithTracer(WithOutput(path)))
	for i := 0; i < 100; i++ {
		s.Next()
	}
	last := s.Next()
	trt.TallyVotes(ctx, last)
	trt.Updates() // just trace final result
	t.Run("live", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, RunTrace(path, nil, WithLogger(logtest.New(t))))
	})
	t.Run("recover", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "tortoise.trace")
		trt, err := Recover(
			context.Background(),
			s.GetState(0).DB,
			last,
			WithTracer(WithOutput(path)),
		)
		require.NoError(t, err)
		trt.Updates()
		require.NoError(t, RunTrace(path, nil, WithLogger(logtest.New(t))))
	})
	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "tortoise.trace")
		trt, err := New(WithTracer(WithOutput(path)))
		require.NoError(t, err)
		ballot := &types.BallotTortoiseData{}
		_, err = trt.DecodeBallot(ballot)
		require.Error(t, err)
		require.NoError(t, RunTrace(path, nil, WithLogger(logtest.New(t))))
	})
}

func TestData(t *testing.T) {
	t.Parallel()
	data, err := filepath.Abs("./data")
	require.NoError(t, err)

	entries, err := os.ReadDir(data)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		t.Skip("directory with data is empty")
	}
	require.NoError(t, err)
	for _, entry := range entries {
		entry := entry
		if strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			t.Parallel()
			require.NoError(
				t,
				RunTrace(filepath.Join(data, entry.Name()), nil, WithLogger(logtest.New(t))),
			)
		})
	}
}
