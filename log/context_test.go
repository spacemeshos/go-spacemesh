package log_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/log"
)

type nopSync struct {
	*bytes.Buffer
}

func (n *nopSync) Sync() error {
	return nil
}

func TestShortUUID(t *testing.T) {
	var buf bytes.Buffer
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		&nopSync{Buffer: &buf},
		zap.NewAtomicLevelAt(zapcore.InfoLevel),
	)
	logger := log.NewFromLog(zap.New(core))
	session := log.WithNewSessionID(context.Background())
	logger.WithContext(session).Info("test")
	type msg struct {
		Session string `json:"sessionId"`
	}
	var m msg
	require.NoError(t, json.Unmarshal(buf.Bytes(), &m))
	require.Len(t, m.Session, 8)
}
