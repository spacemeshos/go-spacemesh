package activation

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestNewPoetListener(t *testing.T) {
	ctrl := gomock.NewController(t)
	poetDb := NewMockpoetValidatorPersister(ctrl)
	lg := logtest.New(t)
	listener := NewPoetListener(poetDb, lg)

	msg := readPoetProofFromDisk(t)
	data, err := codec.Encode(msg)
	require.NoError(t, err)
	ref, err := msg.Ref()
	require.NoError(t, err)

	poetDb.EXPECT().HasProof(ref).Return(false)
	poetDb.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	poetDb.EXPECT().StoreProof(gomock.Any(), ref, gomock.Any()).Return(nil)
	require.Equal(t, pubsub.ValidationAccept, listener.HandlePoetProofMessage(context.Background(), "test", data))

	poetDb.EXPECT().HasProof(ref).Return(false)
	poetDb.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	poetDb.EXPECT().StoreProof(gomock.Any(), ref, gomock.Any()).Return(errors.New("unknown"))
	require.Equal(t, pubsub.ValidationAccept, listener.HandlePoetProofMessage(context.Background(), "test", data))

	poetDb.EXPECT().HasProof(ref).Return(false)
	poetDb.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	poetDb.EXPECT().StoreProof(gomock.Any(), ref, gomock.Any()).Return(sql.ErrObjectExists)
	require.Equal(t, pubsub.ValidationIgnore, listener.HandlePoetProofMessage(context.Background(), "test", data))

	poetDb.EXPECT().HasProof(ref).Return(false)
	poetDb.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("bad poet message"))
	require.Equal(t, pubsub.ValidationIgnore, listener.HandlePoetProofMessage(context.Background(), "test", data))

	poetDb.EXPECT().HasProof(ref).Return(true)
	require.Equal(t, pubsub.ValidationIgnore, listener.HandlePoetProofMessage(context.Background(), "test", data))
}
