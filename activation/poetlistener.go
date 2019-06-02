package activation

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/types"
)

const PoetProofProtocol = "PoetProof"

type PoetListener struct {
	Log               log.Log
	net               service.Service
	poetDb            *PoetDb
	poetProofMessages chan service.GossipMessage
	started           bool
	exit              chan struct{}
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

type poetProofMessage struct {
	types.PoetProof
	PoetId    [types.PoetIdLength]byte
	RoundId   uint64
	Signature []byte
}

func (l *PoetListener) loop() {
	for {
		select {
		case poetProof := <-l.poetProofMessages:
			var proofMessage poetProofMessage
			if err := types.BytesToInterface(poetProof.Bytes(), proofMessage); err != nil {
				l.Log.Error("failed to unmarshal PoET membership proof: %v", err)
				continue
			}
			if err := l.poetDb.ValidateAndStorePoetProof(proofMessage.PoetProof, proofMessage.PoetId,
				proofMessage.RoundId, proofMessage.Signature); err != nil {

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
		Log:               logger,
		net:               net,
		poetDb:            poetDb,
		poetProofMessages: net.RegisterGossipProtocol(PoetProofProtocol),
		exit:              make(chan struct{}),
	}
}
