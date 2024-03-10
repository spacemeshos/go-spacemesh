package types_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestMustBase64EncDecode(t *testing.T) {
	t.Parallel()
	enc := base64.StdEncoding.EncodeToString([]byte("hello"))
	b64 := types.MustBase64FromString(enc)
	require.Equal(t, []byte("hello"), b64.Bytes())
}

func TestMustBase64EncDecodeFail(t *testing.T) {
	t.Parallel()
	require.Panics(t, func() { types.MustBase64FromString("not base64") })
}
