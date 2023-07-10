package metrics

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

func Test_Client(t *testing.T) {
	logger := logtest.New(t, zapcore.InfoLevel)

	genesis, err := time.Parse(time.RFC3339, "2023-07-11T23:00:00.498Z")
	require.NoError(t, err)

	types.SetLayersPerEpoch(60)
	clock, err := timesync.NewClock(
		timesync.WithLayerDuration(2*time.Minute),
		timesync.WithTickInterval(1*time.Second),
		timesync.WithGenesisTime(genesis),
		timesync.WithLogger(logger),
	)
	require.NoError(t, err)

	cfg := beacon.Config{
		ProposalDuration:         1 * time.Minute,
		FirstVotingRoundDuration: 10 * time.Minute,
		VotingRoundDuration:      1 * time.Minute,
		WeakCoinRoundDuration:    1 * time.Minute,
		RoundsNumber:             30,
	}

	c, err := NewClient(
		WithURL("https://mimir.spacemesh.dev/prometheus"),
		WithBeaconConfig(cfg),
		WithLogger(logger),
		WithClock(clock),
		WithBasicAuth(os.Getenv("MIMIR_ORG"), os.Getenv("MIMIR_USR"), os.Getenv("MIMIR_PWD")),
	)
	require.NoError(t, err)

	for epoch := types.EpochID(2); epoch < clock.TimeToLayer(time.Now()).GetEpoch(); epoch++ {
		_, err := c.FetchBeaconValue(context.Background(), "devnet-402-short", epoch)
		require.NoError(t, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// FetchBeaconValue blocks until the beacon for the given epoch is available
	_, err = c.FetchBeaconValue(ctx, "devnet-402-short", clock.CurrentLayer().GetEpoch()+2)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
