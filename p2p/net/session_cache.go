package net

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"sync"
	"time"
)

// simple cache for storing sessions
const maxSessions = 1024

type storedSession struct {
	session NetworkSession
	ts      time.Time
}

type dialSessionFunc func(raddr string, pubkey p2pcrypto.PublicKey) (NetworkSession, error)

type sessionCache struct {
	dialFunc dialSessionFunc
	sMtx     sync.Mutex
	sessions map[string]*storedSession
}

func newSessionCache(dialFunc dialSessionFunc) *sessionCache {
	return &sessionCache{dialFunc: dialFunc,
		sMtx:     sync.Mutex{},
		sessions: make(map[string]*storedSession)}
}

// GetIfExist gets a session from the cache based on address
func (s *sessionCache) GetIfExist(addr string) (NetworkSession, error) {
	s.sMtx.Lock()
	ns, ok := s.sessions[addr]
	s.sMtx.Unlock()
	if ok {
		return ns.session, nil
	}
	return nil, errors.New("not found")
}

// expire removes the oldest entry from the cache. *NOTE*: not thread-safe
func (s *sessionCache) expire() {
	key := ""
	for k, v := range s.sessions {
		if key == "" || v.ts.Before(s.sessions[key].ts) {
			key = k
		}
	}
	delete(s.sessions, key)
}

// GetOrCreate get's a session if it exists, or try to init a new one based on the address and key given.
// *NOTE*: use of dialFunc might actually trigger message sending.
func (s *sessionCache) GetOrCreate(remote string, pubKey p2pcrypto.PublicKey) (NetworkSession, error) {
	s.sMtx.Lock()
	defer s.sMtx.Unlock()
	ns, ok := s.sessions[remote]

	if ok {
		return ns.session, nil
	}

	if len(s.sessions) == maxSessions {
		s.expire()
	}

	session, err := s.dialFunc(remote, pubKey)

	if err != nil {
		return nil, err
	}
	s.sessions[remote] = &storedSession{session, time.Now()}
	return session, nil
}

func (s *sessionCache) handleIncoming(from string, ns NetworkSession) {
	s.sMtx.Lock()
	defer s.sMtx.Unlock()
	_, ok := s.sessions[from]
	if ok {
		// replace ? doesn't matter. should be the same right ?
		return
	}
	if len(s.sessions) == maxSessions {
		s.expire()
	}
	s.sessions[from] = &storedSession{ns, time.Now()}
}
