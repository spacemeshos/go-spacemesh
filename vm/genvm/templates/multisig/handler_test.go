package multisig

import (
	"bytes"
	"testing"

	"github.com/spacemeshos/go-scale"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/vm/genvm/core"
)

func TestDecodeMaxKeys(t *testing.T) {
	keys := make([]core.PublicKey, 11)
	buf := bytes.NewBuffer(nil)
	enc := scale.NewEncoder(buf)
	_, err := scale.EncodeCompact8(enc, 3)
	require.NoError(t, err)
	_, err = scale.EncodeStructSlice(enc, keys)
	require.NoError(t, err)

	args := SpawnArguments{}
	dec := scale.NewDecoder(buf)
	_, err = args.DecodeScale(dec)
	require.ErrorContains(t, err, "11 > 10")
}
