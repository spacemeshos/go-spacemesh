package transaction

import (
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// Convert converts a common transaction to proto transaction
func Convert(t *types.Transaction) *pb.Transaction {
	return &pb.Transaction{
		Id: &pb.TransactionId{Id: t.ID().Bytes()},
		Datum: &pb.Transaction_CoinTransfer{
			CoinTransfer: &pb.CoinTransferTransaction{
				Receiver: &pb.AccountId{Address: t.Recipient.Bytes()},
			},
		},
		Sender: &pb.AccountId{Address: t.Origin().Bytes()},
		GasOffered: &pb.GasOffered{
			// We don't currently implement gas price. t.Fee is the gas actually paid
			// by the tx; GasLimit is the max gas. MeshService is concerned with the
			// pre-STF tx, which includes a gas offer but not an amount of gas actually
			// consumed.
			//GasPrice:    nil,
			GasProvided: t.GasLimit,
		},
		Amount:  &pb.Amount{Value: t.Amount},
		Counter: t.AccountNonce,
		Signature: &pb.Signature{
			Scheme:    pb.Signature_SCHEME_ED25519_PLUS_PLUS,
			Signature: t.Signature[:],
			PublicKey: t.Origin().Bytes(),
		},
	}
}
