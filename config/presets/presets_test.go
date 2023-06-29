package presets

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestCanGeneratePOST(t *testing.T) {
	runTest := func(params config.Config) func(t *testing.T) {
		return func(t *testing.T) {
			req := require.New(t)
			ch := make([]byte, 32)
			cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
			goldenATXID := types.ATXID{2, 3, 4}

			opts := params.SMESHING.Opts
			opts.DataDir = t.TempDir()

			mgr, err := activation.NewPostSetupManager(
				types.EmptyNodeID,
				params.POST,
				logtest.New(t, zapcore.DebugLevel).Named("post"),
				cdb, goldenATXID,
				params.SMESHING.ProvingOpts,
			)
			req.NoError(err)
			req.NoError(mgr.PrepareInitializer(context.Background(), opts))
			req.NoError(mgr.StartSession(context.Background()))

			_, _, err = mgr.GenerateProof(context.Background(), ch)
			req.NoError(err)
		}
	}

	t.Run("fastnet", runTest(fastnet()))

	t.Run("testnet", runTest(testnet()))
}
