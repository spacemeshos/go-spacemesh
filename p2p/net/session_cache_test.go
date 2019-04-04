package net

import (
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/stretchr/testify/require"
	"github.com/syndtr/goleveldb/leveldb/errors"
	"strconv"
	"testing"
	"time"
)

var dialErrFunc dialSessionFunc = func(raddr string, pubkey p2pcrypto.PublicKey) (session NetworkSession, e error) {
	return nil, errors.New("anError")
}

var dialOkFunc dialSessionFunc = func(raddr string, pubkey p2pcrypto.PublicKey) (session NetworkSession, e error) {
	return SessionMock{id: pubkey}, nil
}

//var dialErrFunc dialSessionFunc = func(raddr string, pubkey p2pcrypto.PublicKey) (session NetworkSession, e error) {
//	return nil, errors.New("anError")
//}

func Test_sessionCache_GetIfExist(t *testing.T) {
	sc := newSessionCache(dialErrFunc)
	a, err := sc.GetIfExist("1")
	require.Error(t, err)
	require.Nil(t, a)
	sc.sessions["1"] = &storedSession{SessionMock{}, time.Now()}
	b, err2 := sc.GetIfExist("1")
	require.NoError(t, err2)
	require.NotNil(t, b)
}
func Test_sessionCache_GetOrCreate(t *testing.T) {
	sc := newSessionCache(dialErrFunc)
	rnd := node.GenerateRandomNodeData().PublicKey()
	a, err := sc.GetOrCreate("0.0.0.0:1111", rnd)
	require.Error(t, err)
	require.Nil(t, a)

	sc.dialFunc = dialOkFunc

	b, err2 := sc.GetOrCreate("0.0.0.0:1111", rnd)
	require.NoError(t, err2)
	require.NotNil(t, b)

	nds := node.GenerateRandomNodesData(maxSessions + 10)

	for i := 0; i < len(nds); i++ {
		str := strconv.Itoa(i)
		e, err := sc.GetOrCreate(str, nds[i].PublicKey()) // we don't use address cause there might be dups
		require.NoError(t, err)
		require.NotNil(t, e)
	}

	require.Equal(t, maxSessions, len(sc.sessions))
}

func Test_sessionCache_handleIncoming(t *testing.T) {
	sc := newSessionCache(dialErrFunc)
	sc.handleIncoming("0", SessionMock{})
	require.True(t, len(sc.sessions) == 1)
	sc.handleIncoming("0", SessionMock{})
	require.True(t, len(sc.sessions) == 1)

	nds := node.GenerateRandomNodesData(maxSessions + 10)

	for i := 0; i < len(nds); i++ {
		str := strconv.Itoa(i)
		sc.handleIncoming(str, SessionMock{id: nds[i].PublicKey()}) // we don't use address cause there might be dups
	}

	require.Equal(t, maxSessions, len(sc.sessions))

}
