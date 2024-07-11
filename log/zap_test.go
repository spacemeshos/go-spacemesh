package log_test

import (
	"errors"
	"testing"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func Test_TrimmedErrorField(t *testing.T) {
	t.Parallel()

	err := errors.New("this is a test error message")

	for _, tc := range []struct {
		len         int
		expectedStr string
	}{
		{len: 4, expectedStr: "this..."},
		{len: 10, expectedStr: "this is a ..."},
		{len: 50, expectedStr: "this is a test error message"},
	} {
		t.Run(tc.expectedStr, func(t *testing.T) {
			t.Parallel()

			field := log.TrimmedError(err, tc.len)
			require.Equal(t, "error", field.Key)

			enc := zapcore.NewMapObjectEncoder()
			field.AddTo(enc)

			require.Equal(t, tc.expectedStr, enc.Fields["error"])
		})
	}
}
