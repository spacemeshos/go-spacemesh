package sql

import (
	"fmt"
	"testing"

	"github.com/pkg/errors"
	"github.com/spacemeshos/sqlite"
	"github.com/stretchr/testify/require"
)

func TestDatabase_GetSQLiteError(t *testing.T) {
	t.Parallel()
	sampleErr := sqlite.Error{Code: 1}
	table := []struct {
		name              string
		srcErr            error
		errSqliteExpected *sqlite.Error
	}{
		{name: "nil error", srcErr: nil, errSqliteExpected: nil},
		{name: "not a sqlite error", srcErr: fmt.Errorf("not a sqlite error"), errSqliteExpected: nil},
		{name: "sqlite error", srcErr: sampleErr, errSqliteExpected: &sampleErr},
		{name: "wrapped error", srcErr: errors.Wrap(sampleErr, "wrapped error"), errSqliteExpected: &sampleErr},
		{
			name:              "multiply wrapped error",
			srcErr:            errors.Wrap(errors.Wrap(sampleErr, "wrapped sqlite error"), "wrapped error"),
			errSqliteExpected: &sampleErr,
		},
	}
	for _, tc := range table {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := InMemory().GetSQLiteError(tc.srcErr)
			if tc.errSqliteExpected != nil {
				require.NotNil(t, res)
				require.Equal(t, tc.errSqliteExpected.Code, res.Code)
			} else {
				require.Nil(t, res)
			}
		})
	}
}
