package hare3

import (
	"context"
	"errors"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"go.uber.org/zap"
)

type oracle interface {
	Validate(context.Context, types.LayerID, uint32, int, types.NodeID, types.VrfSignature, uint16) (bool, error)
	CalcEligibility(context.Context, types.LayerID, uint32, int, types.NodeID, types.VrfSignature) (uint16, error)
	Proof(context.Context, types.LayerID, uint32) (types.VrfSignature, error)
}

type legacyOracle struct {
	log    *zap.Logger
	oracle oracle
	config Config
}

func (lg *legacyOracle) validate(msg *Message) grade {
	committee := int(lg.config.Committee)
	if msg.Round == propose {
		committee = int(lg.config.Leaders)
	}
	r := uint32(msg.Iter*uint8(notify)) + uint32(msg.Round)
	valid, err := lg.oracle.Validate(context.Background(),
		msg.Layer, r, committee, msg.Sender,
		msg.Eligibility.Proof, msg.Eligibility.Count)
	if err != nil {
		lg.log.Warn("failed proof validation", zap.Error(err))
		return grade0
	}
	if !valid {
		return grade0
	}
	if msg.Round == propose {
		return grade3
	}
	return grade5
}

func (lg *legacyOracle) active(smesher types.NodeID, layer types.LayerID, ir IterRound) *types.HareEligibility {
	r := uint32(ir.Iter*uint8(notify)) + uint32(ir.Round)
	vrf, err := lg.oracle.Proof(context.Background(), layer, r)
	if err != nil {
		lg.log.Error("failed to compute vrf", zap.Error(err))
		return nil
	}
	committee := int(lg.config.Committee)
	if ir.Round == propose {
		committee = int(lg.config.Leaders)
	}
	count, err := lg.oracle.CalcEligibility(context.Background(), layer, r, committee, smesher, vrf)
	if err != nil {
		if !errors.Is(err, eligibility.ErrNotActive) {
			lg.log.Error("failed to compute eligibilities", zap.Error(err))
		}
		return nil
	}
	return &types.HareEligibility{Proof: vrf, Count: count}
}
