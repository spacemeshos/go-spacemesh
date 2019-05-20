package types

import (
	"github.com/spacemeshos/go-spacemesh/address"
	"math/big"
	"testing"
)

var tx = NewSerializableTransaction(0, address.BytesToAddress([]byte{0x01}), address.BytesToAddress([]byte{0x02}), big.NewInt(10), big.NewInt(10), 10)
var tBytes, _ = TransactionAsBytes(tx)

func BenchmarkTransactionAsBytes(t *testing.B) {
	TransactionAsBytes(tx)
}

func BenchmarkInterfaceToBytes(t *testing.B) {
	InterfaceToBytes(tx)
}

func BenchmarkBytesAsTransaction(t *testing.B) {
	BytesAsTransaction(tBytes)
}

func BenchmarkBytesToInterface(t *testing.B) {
	tx := &SerializableTransaction{}
	BytesToInterface(tBytes, tx)
}
