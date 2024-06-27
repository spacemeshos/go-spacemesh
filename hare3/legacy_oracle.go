package hare3

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare3/eligibility"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type oracle interface {
	Validate(context.Context, types.LayerID, uint32, int, types.NodeID, types.VrfSignature, uint16) (bool, error)
	CalcEligibility(context.Context, types.LayerID, uint32, int, types.NodeID, types.VrfSignature) (uint16, error)
}

type legacyOracle struct {
	log    *zap.Logger
	oracle oracle
	config Config
}

func (lg *legacyOracle) validate(msg *Message) grade {
	if msg.Eligibility.Count == 0 {
		return grade0
	}
	committee := int(lg.config.CommiteeFor(msg.Layer))
	if msg.Round == propose {
		committee = int(lg.config.Leaders)
	}
	valid, err := lg.oracle.Validate(context.Background(),
		msg.Layer, msg.Absolute(), committee, msg.Sender,
		msg.Eligibility.Proof, msg.Eligibility.Count)
	if err != nil {
		lg.log.Warn("failed proof validation", zap.Error(err))
		return grade0
	}
	if !valid {
		return grade0
	}
	return grade5
}

func (lg *legacyOracle) active(
	signer *signing.EdSigner,
	beacon types.Beacon,
	layer types.LayerID,
	ir IterRound,
) *types.HareEligibility {
	vrf := eligibility.GenVRF(context.Background(), signer.VRFSigner(), beacon, layer, ir.Absolute())
	committee := int(lg.config.CommiteeFor(layer))
	if ir.Round == propose {
		committee = int(lg.config.Leaders)
	}
	count, err := lg.oracle.CalcEligibility(context.Background(), layer, ir.Absolute(), committee, signer.NodeID(), vrf)
	if err != nil {
		if !errors.Is(err, eligibility.ErrNotActive) {
			lg.log.Error("failed to compute eligibilities", zap.Error(err))
		} else {
			lg.log.Debug("identity is not active")
		}
		return nil
	}
	if count == 0 {
		return nil
	}
	return &types.HareEligibility{Proof: vrf, Count: count}
}
