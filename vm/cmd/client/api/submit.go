package api

import (
	"context"
	"encoding/hex"
	"fmt"

	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"go.uber.org/zap"
)

func Submit(address string, tx []byte, logger *zap.Logger) error {
	conn, err := connect(address)
	if err != nil {
		return err
	}

	req := spacemeshv2alpha1.SubmitTransactionRequest{
		Transaction: tx,
	}

	resp, err := spacemeshv2alpha1.NewTransactionServiceClient(conn).SubmitTransaction(context.Background(), &req)
	if err != nil {
		return fmt.Errorf("submitting TX: %w", err)
	}
	logger.Info("submitted", zap.String("ID", hex.EncodeToString(resp.TxId)))

	return nil
}
