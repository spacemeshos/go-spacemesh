package types_test

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func init() {
	types.DefaultTestAddressConfig()
}

func TestAddress_NewAddress(t *testing.T) {
	Accounts := map[string]uint64{
		"0x92e27bcd079ed0470307620f1425284c3b2b1a65749c3894": 100000000000000000,
		"0xa64b188ac44b208b1a4f018262a720e6397b092a9abe242a": 100000000000000000,
		"0x889c1c60e278bc6c4f9196ca935b8d60ade7e74da0993c8c": 100000000000000000,
		"0x590f605eaea1e050108b0fbf8d35acc1eae640951e1925f6": 100000000000000000,
		"0x19adfef28a1e88821a6ad44f95301c781853baf8da572636": 100000000000000000,
	}
	for addr := range Accounts {
		genesisAddr, err := types.BytesToAddress(util.FromHex(addr))
		require.NoError(t, err)
		println(genesisAddr.String())
	}
	t.Parallel()
	table := []struct {
		name string
		src  string
		err  error
	}{
		{
			name: "correct",
			src:  "stest1fejq2x3d79ukpkw06t7h6lndjuwzxdnj59npghsg43mh4",
		},
		{
			name: "incorrect cahrset", // chars iob is not supported by bech32.
			src:  "stest1fejq2x3d79ukpkw06t7h6lniobuwzxdnj59nphsywyusj",
			err:  types.ErrDecodeBech32,
		},
		{
			name: "missing hrp",
			src:  "1fejq2x3d79ukpkw06t7h6lniobuwzxdnj59nphsywyusj",
			err:  types.ErrDecodeBech32,
		},
		{
			name: "to big length",
			src:  "stest1qw508d6e1qejxtdg4y5r3zarvax8wucu",
			err:  types.ErrWrongAddressLength,
		},
		{
			name: "to small length",
			src:  "stest1qw504y5r3zarva2v6vda",
			err:  types.ErrWrongAddressLength,
		},
		{
			name: "wrong network",
			src:  "sut1fejq2x3d79ukpkw06t7h6lndjuwzxdnj59npghsldfkky",
			err:  types.ErrUnsupportedNetwork,
		},
	}

	for _, testCase := range table {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := types.FromString(testCase.src)
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
		srcAddr, err := types.FromString("stest1fejq2x3d79ukpkw06t7h6lndjuwzxdnj59npghsg43mh4")
		require.NoError(t, err)
		newAddr, err := types.BytesToAddress(srcAddr.Bytes())
		require.NoError(t, err)
		checkAddressesEqual(t, srcAddr, newAddr)
	})
	t.Run("generate from small byte slice", func(t *testing.T) {
		_, err := types.BytesToAddress([]byte{1})
		require.Error(t, err)
	})
	t.Run("generate from large byte slice length", func(t *testing.T) {
		_, err := types.BytesToAddress([]byte("12345678901234567890123456789012345678901234567890"))
		require.Error(t, err)
	})
	t.Run("generate from correct byte slice", func(t *testing.T) {
		data := make([]byte, types.AddressLength)
		for i := range data {
			data[i] = byte(i)
		}
		newAddr, err := types.BytesToAddress(data)
		require.NoError(t, err)
		srcAddr, err := types.FromString(newAddr.String())
		require.NoError(t, err)
		checkAddressesEqual(t, newAddr, srcAddr)
	})
}

func TestAddress_GenerateAddress(t *testing.T) {
	t.Parallel()
	t.Run("generate from public key", func(t *testing.T) {
		srcAddr := types.GenerateAddress(generatePublicKey(t))
		println(srcAddr.String())
		println(srcAddr.String())
		println(srcAddr.String())
		newAddr, err := types.FromString(srcAddr.String())
		require.NoError(t, err)
		checkAddressesEqual(t, srcAddr, newAddr)
	})
	t.Run("generate from string", func(t *testing.T) {
		srcAddr := types.GenerateAddress([]byte("some random very very long string"))
		newAddr, err := types.FromString(srcAddr.String())
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
	require.Equal(t, addrA.Big(), addrB.Big())
}

func generatePublicKey(t *testing.T) []byte {
	buff := signing.NewEdSigner().ToBuffer()
	acc1Signer, err := signing.NewEdSignerFromBuffer(buff)
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
