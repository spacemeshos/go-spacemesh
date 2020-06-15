package grpc

import (
	"encoding/hex"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/poet/broadcaster/pb"
)

// SubmitTransaction transmits transaction via gossip metwork
func (s NodeService) SubmitTransaction(ctx context.Context, in *pb.SignedTransaction) (*pb.TxConfirmation, error) {
	log.Info("GRPC SubmitTransaction msg")

	tx, err := types.BytesToTransaction(in.Tx)
	if err != nil {
		log.Error("failed to deserialize tx, error %v", err)
		return nil, err
	}
	log.Info("GRPC SubmitTransaction to address: %s (len: %v), amount: %v gaslimit: %v, fee: %v",
		tx.Recipient.Short(), len(tx.Recipient), tx.Amount, tx.GasLimit, tx.Fee)
	if err := tx.CalcAndSetOrigin(); err != nil {
		log.With().Error("failed to calc origin", log.Err(err))
		return nil, err
	}
	if !s.Tx.AddressExists(tx.Origin()) {
		log.With().Error("tx failed to validate signature",
			log.TxID(tx.ID().ShortString()), log.String("origin", tx.Origin().Short()))
		return nil, fmt.Errorf("transaction origin (%v) not found in global state", tx.Origin().Short())
	}
	if err := s.Tx.ValidateNonceAndBalance(tx); err != nil {
		log.With().Error("tx failed nonce and balance check", log.Err(err))
		return nil, err
	}
	log.Info("GRPC SubmitTransaction BROADCAST tx. address %x (len %v), gas limit %v, fee %v id %v nonce %v",
		tx.Recipient, len(tx.Recipient), tx.GasLimit, tx.Fee, tx.ID().ShortString(), tx.AccountNonce)
	go s.Network.Broadcast(miner.IncomingTxProtocol, in.Tx)
	log.Info("GRPC SubmitTransaction returned msg ok")
	return &pb.TxConfirmation{Value: "ok", Id: hex.EncodeToString(tx.ID().Bytes())}, nil
}
