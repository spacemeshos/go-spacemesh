package net

import (
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

var dialErrFunc dialSessionFunc = func(pubkey p2pcrypto.PublicKey) (session NetworkSession) {
	return nil
}

var dialOkFunc dialSessionFunc = func(pubkey p2pcrypto.PublicKey) (session NetworkSession) {
	return SessionMock{id: pubkey}
}

func Test_sessionCache_GetIfExist(t *testing.T) {
	sc := newSessionCache(dialErrFunc)
	pk := p2pcrypto.NewRandomPubkey()
	a, err := sc.GetIfExist(pk)
	require.Error(t, err)
	require.Nil(t, a)
	sc.sessions[pk] = &storedSession{SessionMock{}, time.Now()}
	b, err2 := sc.GetIfExist(pk)
	require.NoError(t, err2)
	require.NotNil(t, b)
}
func Test_sessionCache_GetOrCreate(t *testing.T) {
	sc := newSessionCache(dialErrFunc)
	rnd := node.GenerateRandomNodeData().PublicKey()
	a := sc.GetOrCreate(rnd)
	require.Nil(t, a)

	sc.dialFunc = dialOkFunc

	b := sc.GetOrCreate(rnd)
	require.NotNil(t, b)

	nds := node.GenerateRandomNodesData(maxSessions + 10)

	for i := 0; i < len(nds); i++ {
		//str := strconv.Itoa(i)
		e := sc.GetOrCreate(nds[i].PublicKey()) // we don't use address cause there might be dups
		require.NotNil(t, e)
	}

	require.Equal(t, maxSessions, len(sc.sessions))
}

func Test_sessionCache_handleIncoming(t *testing.T) {
	sc := newSessionCache(dialErrFunc)
	pk := p2pcrypto.NewRandomPubkey()
	sc.handleIncoming(SessionMock{id: pk})
	require.True(t, len(sc.sessions) == 1)
	sc.handleIncoming(SessionMock{id: pk})
	require.True(t, len(sc.sessions) == 1)

	nds := node.GenerateRandomNodesData(maxSessions + 10)

	for i := 0; i < len(nds); i++ {
		sc.handleIncoming(SessionMock{id: nds[i].PublicKey()}) // we don't use address cause there might be dups
	}

	require.Equal(t, maxSessions, len(sc.sessions))

}
