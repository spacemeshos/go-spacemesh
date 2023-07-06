package fetch

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func Fuzz_NewMeshHashRequest(f *testing.F) {
	// examples from https://github.com/spacemeshos/go-spacemesh/issues/4654
	f.Add(uint32(6269), uint32(10265))
	f.Add(uint32(6269), uint32(10149))
	f.Add(uint32(6269), uint32(9248))

	f.Fuzz(func(t *testing.T, from, to uint32) {
		req := NewMeshHashRequest(types.LayerID(from), types.LayerID(to))
		require.NoError(t, req.Validate())
	})
}
