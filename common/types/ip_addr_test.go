package types

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIPAddr(t *testing.T) {
	ip := IPAddr{
		Host:        "192.168.0.0",
		Port:        "80",
		NetworkType: "tcp",
	}

	require.Equal(t, ip.String(), "192.168.0.0:80")
	require.Equal(t, ip.Network(), "tcp")
}

func TestIPAddr_BadString(t *testing.T) {
	ip := IPAddr{
		Host:        "1-631-796-1398",
		Port:        "80",
		NetworkType: "udp",
	}

	require.Equal(t, ip.String(), "not a valid textual representation of an IP address")
}
