package wire_test

import (
	"bytes"
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/spacemeshos/go-scale"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestActivationTxEncoding(t *testing.T) {
	var atx types.ActivationTx
	f := fuzz.NewWithSeed(1001)
	f.Fuzz(&atx)

	buf := bytes.NewBuffer(nil)
	enc := scale.NewEncoder(buf)
	_, err := wire.ActivationTxToWireV1(&atx).EncodeScale(enc)
	require.NoError(t, err)

	var epoch types.EpochID
	_, err = epoch.DecodeScale(scale.NewDecoder(buf))
	require.NoError(t, err)
	require.Equal(t, atx.PublishEpoch, epoch)
}
