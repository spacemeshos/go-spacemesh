package types

import (
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/require"
	"reflect"
	"testing"
)

var alice = newTxUser("alice")
var bob = newTxUser("bob")

var signingTxs = []interface{}{
	OldCoinTx{Amount: 100, Fee: 1, Recipient: alice.Address()},
	SimpleCoinTx{Amount: 101, GasPrice: 1, GasLimit: 10, Recipient: alice.Address()},
	CallAppTx{Amount: 102, GasPrice: 1, GasLimit: 10, AppAddress: alice.Address()},
	SpawnAppTx{Amount: 103, GasPrice: 1, GasLimit: 10, AppAddress: alice.Address()},
}

type txUser struct{ *signing.EdSigner }

func (u txUser) Address() Address {
	return BytesToAddress(u.PublicKey().Bytes())
}

func newTxUser(seedStr string) txUser {
	return txUser{signing.NewEdSignerSeed(seedStr)}
}

var signSchemes = []func(ft interface{}) IncompleteTransaction{
	func(ft interface{}) IncompleteTransaction {
		return ft.(EdTransactionFactory).NewEd()
	},
	func(ft interface{}) IncompleteTransaction {
		return ft.(EdPlusTransactionFactory).NewEdPlus()
	},
}

func txExtract(a interface{}, tx IncompleteTransaction, t *testing.T) (bool, interface{}) {
	val := reflect.ValueOf(a)
	for val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	b := reflect.New(val.Type())
	ok := tx.Extract(b.Interface())
	return ok, b.Elem().Interface()
}

func TestSignTransaction(t *testing.T) {
	for _, scheme := range signSchemes {
		for _, ft := range signingTxs {
			tx := scheme(ft)
			ok, x := txExtract(ft, tx, t)
			require.True(t, ok)
			require.Equal(t, ft, x)
			txm, err := tx.AuthenticationMessage()
			require.NoError(t, err)
			stx, err := txm.Sign(alice.EdSigner)
			require.NoError(t, err)
			rtx, err := stx.Decode()
			require.NoError(t, err)
			ok, x = txExtract(ft, rtx, t)
			require.True(t, ok)
			require.Equal(t, ft, x)
		}
	}
}
