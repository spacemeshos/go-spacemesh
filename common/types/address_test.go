package types_test

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func init() {
	types.DefaultTestAddressConfig()
}

func TestAddress_NewAddress(t *testing.T) {
	t.Parallel()
	table := []struct {
		name string
		src  string
		err  error
	}{
		{
			name: "correct",
			src:  "stest1qqqqqqy0dd83jemjmfj3ghjndxm0ndh0z2rymaqyp0gu3",
		},
		{
			name: "correct address, but no reserved space",
			src:  "stest1fejq2x3d79ukpkw06t7h6lndjuwzxdnj59npghsg43mh4",
			err:  types.ErrMissingReservedSpace,
		},
		{
			name: "incorrect charset", // chars `iob` not supported by bech32.
			src:  "stest1fejq2x3d79ukpkw06t7h6lniobuwzxdnj59nphsywyusj",
			err:  types.ErrDecodeBech32,
		},
		{
			name: "missing hrp",
			src:  "1fejq2x3d79ukpkw06t7h6lniobuwzxdnj59nphsywyusj",
			err:  types.ErrDecodeBech32,
		},
		{
			name: "too long",
			src:  "stest1qw508d6e1qejxtdg4y5r3zarvax8wucu",
			err:  types.ErrWrongAddressLength,
		},
		{
			name: "too short",
			src:  "stest1qw504y5r3zarva2v6vda",
			err:  types.ErrWrongAddressLength,
		},
		{
			name: "wrong network (hrp)",
			src:  "sut1fejq2x3d79ukpkw06t7h6lndjuwzxdnj59npghsldfkky",
			err:  types.ErrUnsupportedNetwork,
		},
	}

	for _, testCase := range table {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := types.StringToAddress(testCase.src)
			if testCase.err != nil {
				require.ErrorContains(t, err, testCase.err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAddress_SetBytes(t *testing.T) {
	t.Parallel()
	t.Run("generate from correct string", func(t *testing.T) {
		srcAddr, err := types.StringToAddress("stest1qqqqqqrs60l66w5uksxzmaznwq6xnhqfv56c28qlkm4a5")
		require.NoError(t, err)
		newAddr := types.GenerateAddress(srcAddr.Bytes())
		require.NoError(t, err)
		checkAddressesEqual(t, srcAddr, newAddr)
	})
	t.Run("generate from small byte slice", func(t *testing.T) {
		newAddr := types.GenerateAddress([]byte{1})
		require.NotEmpty(t, newAddr.String())
	})
	t.Run("generate from large byte slice length", func(t *testing.T) {
		newAddr := types.GenerateAddress([]byte("12345678901234567890123456789012345678901234567890"))
		require.NotEmpty(t, newAddr.String())
	})
	t.Run("generate from correct byte slice", func(t *testing.T) {
		data := make([]byte, types.AddressLength)
		for i := range data {
			data[i] = byte(i)
		}
		newAddr := types.GenerateAddress(data)
		srcAddr, err := types.StringToAddress(newAddr.String())
		require.NoError(t, err)
		checkAddressesEqual(t, newAddr, srcAddr)
	})
}

func TestAddress_GenerateAddress(t *testing.T) {
	t.Parallel()
	t.Run("generate from public key", func(t *testing.T) {
		srcAddr := types.GenerateAddress(generatePublicKey(t))
		newAddr, err := types.StringToAddress(srcAddr.String())
		require.NoError(t, err)
		checkAddressesEqual(t, srcAddr, newAddr)
	})
	t.Run("generate from string", func(t *testing.T) {
		srcAddr := types.GenerateAddress([]byte("some random very very long string"))
		newAddr, err := types.StringToAddress(srcAddr.String())
		require.NoError(t, err)
		checkAddressesEqual(t, srcAddr, newAddr)
	})
}

func TestAddress_ReservedBytesOnTop(t *testing.T) {
	for i := 0; i < 100; i++ {
		addr := types.GenerateAddress(RandomBytes(i))
		for j := 0; j < types.AddressReservedSpace; j++ {
			require.Zero(t, addr.Bytes()[j])
		}
	}
}

func checkAddressesEqual(t *testing.T, addrA, addrB types.Address) {
	require.Equal(t, addrA.Bytes(), addrB.Bytes())
	require.Equal(t, addrA.String(), addrB.String())
	require.Equal(t, addrA.IsEmpty(), addrB.IsEmpty())
}

func generatePublicKey(t *testing.T) []byte {
	buff := signing.NewEdSigner([20]byte{}).ToBuffer()
	acc1Signer, err := signing.NewEdSignerFromBuffer(buff, [20]byte{})
	require.NoError(t, err)
	return acc1Signer.PublicKey().Bytes()
}

// RandomBytes generates random data in bytes for testing.
func RandomBytes(size int) []byte {
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, size)
	_, err := rand.Read(b)
	if err != nil {
		return nil
	}
	return b
}
