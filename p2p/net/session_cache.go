package net

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"sync"
	"time"
)

// simple cache for storing sessions
const maxSessions = 2048

type storedSession struct {
	session NetworkSession
	ts      time.Time
}

type dialSessionFunc func(pubkey p2pcrypto.PublicKey) NetworkSession

type sessionCache struct {
	dialFunc dialSessionFunc
	sMtx     sync.Mutex
	sessions map[p2pcrypto.PublicKey]*storedSession
}

func newSessionCache(dialFunc dialSessionFunc) *sessionCache {
	return &sessionCache{dialFunc: dialFunc,
		sMtx:     sync.Mutex{},
		sessions: make(map[p2pcrypto.PublicKey]*storedSession)}
}

// GetIfExist gets a session from the cache based on address
func (s *sessionCache) GetIfExist(key p2pcrypto.PublicKey) (NetworkSession, error) {
	s.sMtx.Lock()
	ns, ok := s.sessions[key]
	s.sMtx.Unlock()
	if ok {
		return ns.session, nil
	}
	return nil, errors.New("not found")
}

// expire removes the oldest entry from the cache. *NOTE*: not thread-safe
func (s *sessionCache) expire() {
	var key p2pcrypto.PublicKey
	for k, v := range s.sessions {
		if key == nil || v.ts.Before(s.sessions[key].ts) {
			key = k
		}
	}
	delete(s.sessions, key)
}

// GetOrCreate get's a session if it exists, or try to init a new one based on the address and key given.
// *NOTE*: use of dialFunc might actually trigger message sending.
func (s *sessionCache) GetOrCreate(pubKey p2pcrypto.PublicKey) NetworkSession {
	s.sMtx.Lock()
	defer s.sMtx.Unlock()
	ns, ok := s.sessions[pubKey]

	if ok {
		return ns.session
	}

	session := s.dialFunc(pubKey)
	if session == nil {
		return nil
	}

	if len(s.sessions) == maxSessions {
		s.expire()
	}

	s.sessions[pubKey] = &storedSession{session, time.Now()}
	return session
}

func (s *sessionCache) handleIncoming(ns NetworkSession) {
	s.sMtx.Lock()
	defer s.sMtx.Unlock()
	_, ok := s.sessions[ns.ID()]
	if ok {
		// replace ? doesn't matter. should be the same
		return
	}
	if len(s.sessions) == maxSessions {
		s.expire()
	}
	s.sessions[ns.ID()] = &storedSession{ns, time.Now()}
}
