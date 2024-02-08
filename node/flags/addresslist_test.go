package flags

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/p2p"
)

func TestAddressList(t *testing.T) {
	al := p2p.MustParseAddresses("/ip4/0.0.0.0/tcp/7513")
	alv := NewAddressListValue(al, &al)
	require.Equal(t, []string{"/ip4/0.0.0.0/tcp/7513"}, alv.GetSlice())
	require.Equal(t, "addressList", alv.Type())
	require.Equal(t, "[/ip4/0.0.0.0/tcp/7513]", alv.String())
	require.NoError(t, alv.Set("[/ip4/0.0.0.0/tcp/5000]")) // replace the default value
	require.Equal(t, "[/ip4/0.0.0.0/tcp/5000]", alv.String())
	require.NoError(t, alv.Set("/ip4/0.0.0.0/udp/0/quic-v1,/ip4/0.0.0.0/tcp/5002")) // append more values
	require.Equal(t, "[/ip4/0.0.0.0/tcp/5000,/ip4/0.0.0.0/udp/0/quic-v1,/ip4/0.0.0.0/tcp/5002]", alv.String())
	require.NoError(t, alv.Replace([]string{"/ip4/0.0.0.0/tcp/4999"}))
	require.NoError(t, alv.Append("/ip4/0.0.0.0/tcp/5001"))
	require.Equal(t, "[/ip4/0.0.0.0/tcp/4999,/ip4/0.0.0.0/tcp/5001]", alv.String())

	require.Error(t, alv.Set("abc,def"))
	require.Error(t, alv.Replace([]string{"foobar"}))
	require.Error(t, alv.Append("barbaz"))
	require.Equal(t, "[/ip4/0.0.0.0/tcp/4999,/ip4/0.0.0.0/tcp/5001]", alv.String())
}
