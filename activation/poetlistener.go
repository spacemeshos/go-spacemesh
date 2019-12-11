package activation

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

const PoetProofProtocol = "PoetProof"

type PoetValidatorPersistor interface {
	Validate(proof types.PoetProof, poetId []byte, roundId string, signature []byte) error
	storeProof(proofMessage *types.PoetProofMessage) error
}

type PoetListener struct {
	Log               log.Log
	net               service.Service
	poetDb            PoetValidatorPersistor
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

func (l *PoetListener) loop() {
	for {
		select {
		case poetProof := <-l.poetProofMessages:
			if poetProof == nil {
				log.Error("nil poet message received!")
				continue
			}
			go l.handlePoetProofMessage(poetProof)
		case <-l.exit:
			l.Log.Info("listening stopped")
			return
		}
	}
}

func (l *PoetListener) handlePoetProofMessage(gossipMessage service.GossipMessage) {
	// ⚠️ IMPORTANT: We must not ensure that the node is synced! PoET messages must be propagated regardless.
	var proofMessage types.PoetProofMessage
	if err := types.BytesToInterface(gossipMessage.Bytes(), &proofMessage); err != nil {
		l.Log.Error("failed to unmarshal PoET membership proof: %v", err)
		return
	}
	if err := l.poetDb.Validate(proofMessage.PoetProof, proofMessage.PoetServiceId,
		proofMessage.RoundId, proofMessage.Signature); err != nil {

		if types.IsProcessingError(err) {
			l.Log.Error("failed to validate PoET proof: %v", err)
		} else {
			l.Log.Warning("PoET proof not valid: %v", err)
		}
		return
	}

	gossipMessage.ReportValidation(PoetProofProtocol)

	if err := l.poetDb.storeProof(&proofMessage); err != nil {
		l.Log.Error("failed to store PoET proof: %v", err)
	}
}

func NewPoetListener(net service.Service, poetDb PoetValidatorPersistor, logger log.Log) *PoetListener {
	return &PoetListener{
		Log:               logger,
		net:               net,
		poetDb:            poetDb,
		poetProofMessages: net.RegisterGossipProtocol(PoetProofProtocol),
		exit:              make(chan struct{}),
	}
}
