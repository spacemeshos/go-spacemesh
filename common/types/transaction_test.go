package types

import (
	"bufio"
	"bytes"
	"crypto/sha512"
	"fmt"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"reflect"
	"regexp"
	"testing"
)

var alice = newTxUser("alice")

var signingTxs = []interface{}{
	OldCoinTx{Amount: 100, Fee: 1, Recipient: alice.Address()},
	SimpleCoinTx{Amount: 101, GasPrice: 1, GasLimit: 10, Recipient: alice.Address()},
	CallAppTx{Amount: 102, GasPrice: 1, GasLimit: 10, AppAddress: alice.Address(), CallData: []byte{}},
	SpawnAppTx{Amount: 103, GasPrice: 1, GasLimit: 10, AppAddress: alice.Address(), CallData: []byte{}},
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

func txExtract(a interface{}, tx CommonTransaction, t *testing.T) (bool, interface{}) {
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

func TestVerifyTransaction(t *testing.T) {
	for _, scheme := range signSchemes {
		for _, ft := range signingTxs {
			itx := scheme(ft)
			tx, err := SignTransaction(itx, alice.EdSigner)
			require.NoError(t, err)
			ok, x := txExtract(ft, tx, t)
			require.True(t, ok)
			require.Equal(t, ft, x)
			txm, err := tx.AuthenticationMessage()
			require.NoError(t, err)
			ok = txm.Verify(tx.PubKey(), tx.Signature())
			require.True(t, ok)
		}
	}
}

func TestVerifyTransactionType(t *testing.T) {
	ft := signingTxs[0]
	itx := signSchemes[0](ft)
	tx, err := SignTransaction(itx, alice.EdSigner)
	require.NoError(t, err)
	ok, x := txExtract(ft, tx, t)
	require.True(t, ok)
	require.Equal(t, ft, x)
	txm, err := tx.AuthenticationMessage()
	require.NoError(t, err)
	stx, err := txm.Encode(tx.PubKey(), tx.Signature())
	require.NoError(t, err)
	stx[0] = 0xff
	_, err = stx.Decode()
	require.EqualError(t, err, errBadTransactionTypeError.Error())
	stx[0] = 0
	stx[2] = 0xff
	_, err = stx.Decode()
	require.Error(t, err)
}

func TestVerifyTransactionLength(t *testing.T) {
	ft := signingTxs[0]
	for _, scheme := range signSchemes {
		itx := scheme(ft)
		tx, err := SignTransaction(itx, alice.EdSigner)
		require.NoError(t, err)
		ok, x := txExtract(ft, tx, t)
		require.True(t, ok)
		require.Equal(t, ft, x)
		txm, err := tx.AuthenticationMessage()
		require.NoError(t, err)
		stx, err := txm.Encode(tx.PubKey(), tx.Signature())
		require.NoError(t, err)
		stx = append(stx, 0)
		_, err = stx.Decode()
		require.EqualError(t, err, errBadTransactionEncodingError.Error())
		stx = append(stx, 0, 0, 0)
		_, err = stx.Decode()
		require.EqualError(t, err, errBadTransactionEncodingError.Error())
		stx = append(stx, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9)
		_, err = stx.Decode()
		require.EqualError(t, err, errBadTransactionEncodingError.Error())
	}
}

func TestVerifyTransactionBody(t *testing.T) {
	ft := signingTxs[0]
	itx := ft.(EdPlusTransactionFactory).NewEdPlus()
	tx, err := SignTransaction(itx, alice.EdSigner)
	require.NoError(t, err)
	ok, x := txExtract(ft, tx, t)
	require.True(t, ok)
	require.Equal(t, ft, x)
	txm, err := tx.AuthenticationMessage()
	require.NoError(t, err)
	txm.TransactionData = append(txm.TransactionData, 0, 1, 2, 3, 4)
	stx, err := txm.Encode(tx.PubKey(), tx.Signature())
	require.NoError(t, err)
	_, err = stx.Decode()
	require.EqualError(t, err, errBadTransactionEncodingError.Error())
}

func TestSignEdPlus(t *testing.T) {
	m := sha512.Sum512([]byte("dead beef"))
	sig := alice.Sign2(m[:])
	fmt.Println("mesg1:", util.Bytes2Hex(m[:]))
	fmt.Println("pubk1:", alice.PublicKey())
	m2 := sha512.Sum512([]byte("beef dead"))
	pk, err := ed25519.ExtractPublicKey(m2[:], sig)
	require.NoError(t, err)
	fmt.Println("mesg2:", util.Bytes2Hex(m2[:]))
	fmt.Println("pubk2:", signing.NewPublicKey(pk))
	ok := ed25519.Verify2(pk, m2[:], sig)
	fmt.Println("signature is OK? ", ok)
	require.True(t, ok)
}

func _TestG1(t *testing.T) {
	bf := bytes.Buffer{}
	wr := bufio.NewWriter(&bf)
	bob := newTxUser("bob")
	charlie := newTxUser("bob")
	re := regexp.MustCompile("..")
	lx := []string{}
	b2x := func(b []byte) string {
		return re.ReplaceAllString(util.Bytes2Hex(b), "0x$0,")
	}
	wrs := func(s string) {
		_, _ = wr.WriteString(s)
	}
	wrf := func(s string, a ...interface{}) {
		wrs(fmt.Sprintf(s, a...))
	}
	p2w := func(txt string, bs []byte) {
		wrs("/*" + txt + "*/ " + b2x(bs) + "\n")
	}
	edfx := func(tx IncompleteTransaction, n string) {
		txm, err := tx.AuthenticationMessage()
		require.NoError(t, err)
		sxm, err := txm.Sign(alice.EdSigner)
		require.NoError(t, err)
		wrf("var txCase%s = SignedTransaction{\n", n)
		p2w("type:", sxm[0:4])
		p2w("sign:", sxm[4:68])
		l1 := (int(sxm[68+3])+3)&^3 + 4 + 68
		p2w("data:", sxm[68:l1])
		p2w("pkey:", sxm[l1:])
		wrs("}\n")
	}
	txf := func(itx interface{}, n string) {
		v := reflect.ValueOf(itx)
		vt := v.Type()
		wrf("var txValue%s = %s{\n", n, vt.Name())
		for i := 0; i < vt.NumField(); i++ {
			f := vt.Field(i)
			if f.Type == reflect.TypeOf(Address{}) {
				a := v.Field(i).Interface().(Address)
				wrf("\t%s: Address{%v},\n", f.Name, b2x(a[:]))
			} else if f.Type == reflect.TypeOf([]byte{}) {
				if v.Field(i).IsNil() || v.Field(i).Len() == 0 {
					wrf("\t%s: []byte{},\n", f.Name)
				} else {
					a := v.Field(i).Interface().([]byte)
					wrf("\t%s: []byte{%v},\n", f.Name, b2x(a[:]))
				}
			} else {
				wrf("\t%s: %v,\n", f.Name, v.Field(i).Interface())
			}
		}
		wrs("}\n")
	}
	edf := func(itx interface{}, n string) {
		txf(itx, n)
		lx = append(lx, n)
		edfx(itx.(EdTransactionFactory).NewEd(), n+"Ed")
		edfx(itx.(EdPlusTransactionFactory).NewEdPlus(), n+"EdPlus")
	}

	wrf(`
package types
import (
	"github.com/stretchr/testify/require"
	"reflect"
	"testing"
)

// PrivKey = %v
// PubKey = %v

`, util.Bytes2Hex(alice.ToBuffer()), util.Bytes2Hex(alice.PublicKey().Bytes()))

	edf(OldCoinTx{Amount: 100, GasLimit: 10, AccountNonce: 5, Fee: 1, Recipient: bob.Address()}, "OldCoinTx1")
	edf(OldCoinTx{Amount: 101, GasLimit: 10, AccountNonce: 6, Fee: 2, Recipient: charlie.Address()}, "OldCoinTx2")
	edf(SimpleCoinTx{Amount: 102, GasLimit: 1, GasPrice: 10, Nonce: 5, Recipient: bob.Address()}, "SimpleCoinTx1")
	edf(SimpleCoinTx{Amount: 103, GasLimit: 10, GasPrice: 1, Nonce: 8, Recipient: charlie.Address()}, "SimpleCoinTx2")
	edf(CallAppTx{Amount: 104, GasLimit: 1, GasPrice: 10, Nonce: 5, AppAddress: bob.Address(), CallData: []byte{1, 2, 3, 4, 5, 6}}, "CallAppTx1")
	edf(CallAppTx{Amount: 105, GasLimit: 1, GasPrice: 5, Nonce: 8, AppAddress: charlie.Address(), CallData: []byte{}}, "CallAppTx2")
	edf(CallAppTx{Amount: 106, GasLimit: 1, GasPrice: 33, Nonce: 12, AppAddress: alice.Address(), CallData: nil}, "CallAppTx3")
	edf(SpawnAppTx{Amount: 107, GasLimit: 1, GasPrice: 10, Nonce: 5, AppAddress: bob.Address(), CallData: []byte{1, 2, 3, 4, 5, 6}}, "SpawnAppTx1")
	edf(SpawnAppTx{Amount: 108, GasLimit: 1, GasPrice: 5, Nonce: 8, AppAddress: charlie.Address(), CallData: []byte{}}, "SpawnAppTx2")
	edf(SpawnAppTx{Amount: 109, GasLimit: 1, GasPrice: 33, Nonce: 12, AppAddress: alice.Address(), CallData: nil}, "SpawnAppTx3")

	wrf("var txCases = []struct{Tx interface{}; Signed SignedTransaction}{\n")
	for _, v := range lx {
		wrf("\t{txValue%s,txCase%sEd},\n", v, v)
		wrf("\t{txValue%s,txCase%sEdPlus},\n", v, v)
	}
	wrs("}\n")

	wrs(`	
func TestBinaryTransactions(t *testing.T) {
	for _,v := range txCases {
		tx, err := v.Signed.Decode()
		require.NoError(t, err)
		val := reflect.ValueOf(v.Tx)
		b := reflect.New(val.Type())
		ok := tx.Extract(b.Interface())
		require.True(t, ok)
		require.Equal(t, v.Tx, b.Elem().Interface())
	}
}	
`)
	_ = wr.Flush()
	_ = ioutil.WriteFile("transactionbin_test.go", bf.Bytes(), 0666)
}
