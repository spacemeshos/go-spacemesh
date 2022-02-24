package activation

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestNewPoetListener(t *testing.T) {
	ctrl := gomock.NewController(t)
	poetDb := mocks.NewMockpoetValidatorPersistor(ctrl)
	lg := logtest.New(t)
	listener := NewPoetListener(poetDb, lg)

	msg, err := types.InterfaceToBytes(&types.PoetProofMessage{})
	require.NoError(t, err)
	poetDb.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	poetDb.EXPECT().StoreProof(gomock.Any()).Return(nil)
	require.Equal(t, pubsub.ValidationAccept, listener.HandlePoetProofMessage(context.TODO(), "test", msg))

	poetDb.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	poetDb.EXPECT().StoreProof(gomock.Any()).Return(errors.New("unknown"))
	require.Equal(t, pubsub.ValidationAccept, listener.HandlePoetProofMessage(context.TODO(), "test", msg))

	poetDb.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	poetDb.EXPECT().StoreProof(gomock.Any()).Return(sql.ErrObjectExists)
	require.Equal(t, pubsub.ValidationIgnore, listener.HandlePoetProofMessage(context.TODO(), "test", msg))

	poetDb.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("bad poet message"))
	require.Equal(t, pubsub.ValidationIgnore, listener.HandlePoetProofMessage(context.TODO(), "test", msg))
}
