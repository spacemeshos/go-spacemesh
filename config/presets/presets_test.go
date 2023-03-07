package presets

import (
	"context"
	"testing"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/require"
)

func TestCanGeneratePOST(t *testing.T) {
	runTest := func(t *testing.T, params config.Config) {
		req := require.New(t)
		ch := make([]byte, 32)
		cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
		goldenATXID := types.ATXID{2, 3, 4}

		opts := params.SMESHING.Opts
		opts.DataDir = t.TempDir()
		opts.MaxFileSize = 4096
		opts.ComputeProviderID = int(initialization.CPUProviderID())

		mgr, err := activation.NewPostSetupManager(types.NodeID{}, params.POST, logtest.New(t), cdb, goldenATXID)
		req.NoError(err)
		req.NoError(mgr.StartSession(context.Background(), opts, goldenATXID))

		_, _, err = mgr.GenerateProof(context.Background(), ch)
		req.NoError(err)
	}

	t.Run("fastnet", func(t *testing.T) {
		runTest(t, fastnet())
	})
	t.Run("testnet", func(t *testing.T) {
		runTest(t, testnet())
	})

}
