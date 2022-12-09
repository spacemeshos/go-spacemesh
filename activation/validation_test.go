package activation

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func Test_ValidateVRFNonce(t *testing.T) {
	r := require.New(t)

	// Arrange
	ctrl := gomock.NewController(t)
	poetDbAPI := NewMockpoetDbAPI(ctrl)
	postCfg := DefaultPostConfig()
	meta := &types.PostMetadata{
		BitsPerLabel:  postCfg.BitsPerLabel,
		LabelsPerUnit: postCfg.LabelsPerUnit,
	}

	initOpts := DefaultPostSetupOpts()
	initOpts.DataDir = t.TempDir()
	initOpts.ComputeProviderID = int(initialization.CPUProviderID())

	nodeId := types.BytesToNodeID(make([]byte, 32))
	commitmentAtxId := types.RandomATXID()

	init, err := initialization.NewInitializer(
		initialization.WithNodeId(nodeId.Bytes()),
		initialization.WithCommitmentAtxId(commitmentAtxId.Bytes()),
		initialization.WithConfig((config.Config)(postCfg)),
		initialization.WithInitOpts((config.InitOpts)(initOpts)),
	)
	r.NoError(err)
	r.NoError(init.Initialize(context.Background()))
	r.NotNil(init.Nonce())

	nonce := (*types.VRFPostIndex)(init.Nonce())

	v := NewValidator(poetDbAPI, postCfg)

	// Act & Assert
	t.Run("valid vrf nonce", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, v.ValidateVRFNonce(nodeId, commitmentAtxId, nonce, meta, initOpts.NumUnits))
	})

	t.Run("invalid vrf nonce", func(t *testing.T) {
		t.Parallel()

		nonce := types.VRFPostIndex(1)
		require.Error(t, v.ValidateVRFNonce(nodeId, commitmentAtxId, &nonce, meta, initOpts.NumUnits))
	})

	t.Run("wrong commitmentAtxId", func(t *testing.T) {
		t.Parallel()

		commitmentAtxId := types.RandomATXID()
		require.Error(t, v.ValidateVRFNonce(nodeId, commitmentAtxId, nonce, meta, initOpts.NumUnits))
	})

	t.Run("numUnits can be smaller or larger", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, v.ValidateVRFNonce(nodeId, commitmentAtxId, nonce, meta, initOpts.NumUnits+1))

		require.NoError(t, v.ValidateVRFNonce(nodeId, commitmentAtxId, nonce, meta, initOpts.NumUnits-1))
	})
}
