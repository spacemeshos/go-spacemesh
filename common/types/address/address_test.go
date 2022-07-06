package address_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types/address"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestAddress_NewAddress(t *testing.T) {
	t.Parallel()
	table := []struct {
		name string
		src  string
		err  error
	}{
		{
			name: "correct",
			src:  "sm1fejq2x3d79ukpkw06t7h6lndjuwzxdnj59npghs4qgrev",
		},
		{
			name: "wrong length",
			src:  "smt1qw508d6e1qejxtdg4y5r3zarvaq9lg46",
			err:  address.ErrWrongAddressLength,
		},
		{
			name: "wrong network",
			src:  "sut1fejq2x3d79ukpkw06t7h6lndjuwzxdnj59npghsldfkky",
			err:  address.ErrUnsupportedNetwork,
		},
	}

	for _, testCase := range table {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := address.NewAddress(testCase.src)
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
		srcAddr, err := address.NewAddress("sm1fejq2x3d79ukpkw06t7h6lndjuwzxdnj59npghs4qgrev")
		require.NoError(t, err)
		newAddr, err := address.BytesToAddress(srcAddr.Bytes())
		require.NoError(t, err)
		checkAddressesEqual(t, srcAddr, newAddr)
	})
	t.Run("generate from small byte slice", func(t *testing.T) {
		_, err := address.BytesToAddress([]byte{1})
		require.Error(t, err)
	})
	t.Run("generate from large byte slice length", func(t *testing.T) {
		_, err := address.BytesToAddress([]byte("12345678901234567890123456789012345678901234567890"))
		require.Error(t, err)
	})
	t.Run("generate from correct byte slice", func(t *testing.T) {
		data := make([]byte, address.AddressLength+address.HumanReadablePartLength)
		for i := range data {
			data[i] = byte(i)
		}
		data[0] = byte(address.MainnetID)
		newAddr, err := address.BytesToAddress(data)
		require.NoError(t, err)
		srcAddr, err := address.NewAddress(newAddr.String())
		require.NoError(t, err)
		checkAddressesEqual(t, newAddr, srcAddr)
	})
}

func TestAddress_GenerateAddress(t *testing.T) {
	t.Parallel()
	t.Run("generate from public key", func(t *testing.T) {
		srcAddr, err := address.GenerateAddress(address.TestnetID, generatePublicKey(t))
		require.NoError(t, err)
		newAddr, err := address.NewAddress(srcAddr.String())
		require.NoError(t, err)
		checkAddressesEqual(t, srcAddr, newAddr)
	})
	t.Run("generate from string", func(t *testing.T) {
		srcAddr, err := address.GenerateAddress(address.TestnetID, []byte("some random very very long string"))
		require.NoError(t, err)
		newAddr, err := address.NewAddress(srcAddr.String())
		require.NoError(t, err)
		checkAddressesEqual(t, srcAddr, newAddr)
	})
}

func TestAddress_ByteToAddress(t *testing.T) {
	t.Parallel()
	addr, err := address.ByteToAddress(address.TestnetID, 2)
	require.NoError(t, err)
	require.Equal(t, uint8(address.TestnetID), addr[0])
	require.Equal(t, uint8(2), addr[1])
	for _, b := range addr[2:] {
		require.Equal(t, uint8(0), b)
	}
}

func checkAddressesEqual(t *testing.T, addrA, addrB address.Address) {
	require.Equal(t, addrA.Bytes(), addrB.Bytes())
	require.Equal(t, addrA.String(), addrB.String())
	require.Equal(t, addrA.Short(), addrB.Short())
	require.Equal(t, addrA.Big(), addrB.Big())
}

func generatePublicKey(t *testing.T) []byte {
	buff := signing.NewEdSigner().ToBuffer()
	acc1Signer, err := signing.NewEdSignerFromBuffer(buff)
	require.NoError(t, err)
	return acc1Signer.PublicKey().Bytes()
}

//func intToAddress(t *testing.T, num uint32) *address.Address {
//	data := make([]byte, 4)
//	binary.LittleEndian.PutUint32(data, num)
//	var addrBytes [address.AddressLength]byte
//	copy(addrBytes[:], data)
//	addr, err := address.GenerateAddress(address.TestnetID, addrBytes[:])
//	require.NoError(t, err)
//	return addr
//}
