package v2alpha1

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPoetServices(t *testing.T) {
	t.Run("no mismatch")
	db := sql.InMemory()
	ctx := context.Background()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	svc := NewSmeshingIdentitiesService(db)
}
