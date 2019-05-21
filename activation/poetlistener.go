package activation

import (
	"bytes"
	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/types"
)

const (
	PoetMembershipProofProtocol = "PoetMembershipProof"
	PoetProofProtocol           = "PoetProof"
)

type PoetListener struct {
	Log                     log.Log
	net                     service.Service
	poetDb                  *PoetDb
	membershipProofMessages chan service.GossipMessage
	poetProofMessages       chan service.GossipMessage
	started                 bool
	exit                    chan struct{}
}

func (l *PoetListener) Start() {
	if l.started {
		return
	}
	go l.loop()
	l.started = true
}

func (l *PoetListener) Close() {
	close(l.exit)
	l.started = false
}

func (l *PoetListener) loop() {
	for {
		select {
		case membershipProof := <-l.membershipProofMessages:
			var proof types.PoetMembershipProof
			if _, err := xdr.Unmarshal(bytes.NewReader(membershipProof.Bytes()), proof); err != nil {
				l.Log.Error("failed to unmarshal PoET membership proof: %v", err)
				continue
			}
			// TODO(noamnelke): validate proof
			if err := l.poetDb.AddMembershipProof(proof); err != nil {
				l.Log.Warning("PoET membership proof not persisted: %v", err)
				continue
			}
			membershipProof.ReportValidation(PoetMembershipProofProtocol)
		case poetProof := <-l.poetProofMessages:
			var proof types.PoetProof
			_, err := xdr.Unmarshal(bytes.NewReader(poetProof.Bytes()), proof)
			if err != nil {
				l.Log.Error("failed to unmarshal PoET membership proof: %v", err)
				continue
			}
			// TODO(noamnelke): validate proof
			if err := l.poetDb.AddPoetProof(proof); err != nil {
				l.Log.Warning("PoET proof not persisted: %v", err)
				continue
			}
			poetProof.ReportValidation(PoetProofProtocol)
		case <-l.exit:
			l.Log.Info("listening stopped")
			return
		}
	}
}

func NewPoetListener(net service.Service, poetDb *PoetDb, logger log.Log) *PoetListener {
	return &PoetListener{
		Log:                     logger,
		net:                     net,
		poetDb:                  poetDb,
		membershipProofMessages: net.RegisterGossipProtocol(PoetMembershipProofProtocol),
		poetProofMessages:       net.RegisterGossipProtocol(PoetProofProtocol),
		exit:                    make(chan struct{}),
	}
}
