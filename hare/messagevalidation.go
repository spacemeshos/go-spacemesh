package hare

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"time"
)

type messageValidator interface {
	SyntacticallyValidateMessage(ctx context.Context, m *Msg) bool
	ContextuallyValidateMessage(ctx context.Context, m *Msg, expectedK int32) error
}

type identityProvider interface {
	GetIdentity(edID string) (types.NodeID, error)
}

type eligibilityValidator struct {
	oracle           Rolacle
	layersPerEpoch   uint16
	identityProvider identityProvider
	maxExpActives    int // the maximal expected committee size
	expLeaders       int // the expected number of leaders
	log.Log
}

func newEligibilityValidator(oracle Rolacle, layersPerEpoch uint16, idProvider identityProvider, maxExpActives, expLeaders int, logger log.Log) *eligibilityValidator {
	return &eligibilityValidator{oracle, layersPerEpoch, idProvider, maxExpActives, expLeaders, logger}
}

// check eligibility of the provided message by the oracle.
func (ev *eligibilityValidator) validateRole(ctx context.Context, m *Msg) (bool, error) {
	logger := ev.WithContext(ctx)

	if m == nil {
		logger.Error("eligibility validator: called with nil")
		return false, errors.New("fatal: nil message")
	}

	if m.InnerMsg == nil {
		logger.Error("eligibility validator: InnerMsg is nil")
		return false, errors.New("fatal: nil inner message")
	}

	pub := m.PubKey
	layer := types.LayerID(m.InnerMsg.InstanceID)
	if layer.GetEpoch().IsGenesis() {
		return true, nil // TODO: remove this lie after inception problem is addressed
	}

	nID, err := ev.identityProvider.GetIdentity(pub.String())
	if err != nil {
		logger.With().Error("eligibility validator: GetIdentity failed (ignore if the safe layer is in genesis)",
			log.Err(err),
			log.String("sender_id", pub.ShortString()))
		return false, err
	}

	// validate role
	res, err := ev.oracle.Validate(ctx, layer, m.InnerMsg.K, expectedCommitteeSize(m.InnerMsg.K, ev.maxExpActives, ev.expLeaders), nID, m.InnerMsg.RoleProof, m.InnerMsg.EligibilityCount)
	if err != nil {
		logger.With().Error("eligibility validator: could not retrieve eligibility result",
			log.Err(err),
			log.String("sender_id", pub.ShortString()))
		return false, err
	}
	if !res {
		logger.With().Error("eligibility validator: sender is not eligible to participate",
			log.String("sender_id", pub.ShortString()))
		return false, nil
	}

	return true, nil
}

// Validate the eligibility of the provided message.
func (ev *eligibilityValidator) Validate(ctx context.Context, m *Msg) bool {
	res, err := ev.validateRole(ctx, m)
	if err != nil {
		ev.WithContext(ctx).With().Error("error occurred while validating role",
			log.Err(err),
			log.String("sender_id", m.PubKey.ShortString()),
			types.LayerID(m.InnerMsg.InstanceID),
			log.String("msg_type", m.InnerMsg.Type.String()))
		return false
	}

	// verify role
	if !res {
		ev.WithContext(ctx).With().Warning("validate message failed: role is invalid",
			log.String("sender_id", m.PubKey.ShortString()),
			types.LayerID(m.InnerMsg.InstanceID),
			log.String("msg_type", m.InnerMsg.Type.String()))
		return false
	}

	return true
}

type roleValidator interface {
	Validate(context.Context, *Msg) bool
}

type pubKeyGetter interface {
	Track(*Msg)
	PublicKey(*Message) *signing.PublicKey
}

type syntaxContextValidator struct {
	signing          Signer
	threshold        int
	statusValidator  func(m *Msg) bool // used to validate status Messages in SVP
	stateQuerier     StateQuerier
	layersPerEpoch   uint16
	roleValidator    roleValidator
	validMsgsTracker pubKeyGetter // used to check for public keys in the valid messages tracker
	log.Log
}

func newSyntaxContextValidator(sgr Signer, threshold int, validator func(m *Msg) bool, stateQuerier StateQuerier, layersPerEpoch uint16, ev roleValidator, validMsgsTracker pubKeyGetter, logger log.Log) *syntaxContextValidator {
	return &syntaxContextValidator{sgr, threshold, validator, stateQuerier, layersPerEpoch, ev, validMsgsTracker, logger}
}

// contextual validation errors
var (
	errNilMsg         = errors.New("nil message")
	errNilInner       = errors.New("nil inner message")
	errEarlyMsg       = errors.New("early message")
	errInvalidIter    = errors.New("incorrect iteration number")
	errInvalidRound   = errors.New("incorrect round")
	errUnexpectedType = errors.New("unexpected message type")
)

// ContextuallyValidateMessage checks if the message is contextually valid.
// Returns nil if the message is contextually valid or a suitable error otherwise.
func (v *syntaxContextValidator) ContextuallyValidateMessage(ctx context.Context, m *Msg, currentK int32) error {
	if m == nil {
		return errNilMsg
	}

	if m.InnerMsg == nil {
		return errNilInner
	}
	currentRound := currentK % 4
	// the message must match the current iteration unless it is a notify or pre-round message
	currentIteration := currentK / 4
	msgIteration := m.InnerMsg.K / 4
	sameIter := currentIteration == msgIteration

	// first validate pre-round and notify
	switch m.InnerMsg.Type {
	case pre:
		return nil
	case notify:
		// notify before notify could be created for this iteration
		if currentRound < commitRound && sameIter {
			return errInvalidRound
		}

		// old notify is accepted
		if m.InnerMsg.K <= currentK {
			return nil
		}

		// early notify detected
		if m.InnerMsg.K == currentK+1 && currentRound == commitRound {
			return errEarlyMsg
		}

		// future notify is rejected
		return errInvalidIter
	}

	// check status, proposal & commit types
	switch m.InnerMsg.Type {
	case status:
		if currentRound == preRound && sameIter {
			return errEarlyMsg
		}
		if currentRound == notifyRound && currentIteration+1 == msgIteration {
			return errEarlyMsg
		}
		if currentRound == statusRound && sameIter {
			return nil
		}
		if !sameIter {
			return errInvalidIter
		}
		return errInvalidRound
	case proposal:
		if currentRound == statusRound && sameIter {
			return errEarlyMsg
		}
		// a late proposal is also contextually valid
		if (currentRound == proposalRound || currentRound == commitRound) && sameIter {
			return nil
		}
		if !sameIter {
			return errInvalidIter
		}
		return errInvalidRound
	case commit:
		if currentRound == proposalRound && sameIter {
			return errEarlyMsg
		}
		if currentRound == commitRound && sameIter {
			return nil
		}
		if !sameIter {
			return errInvalidIter
		}
		return errInvalidRound
	}

	return errUnexpectedType
}

// SyntacticallyValidateMessage the syntax of the provided message.
func (v *syntaxContextValidator) SyntacticallyValidateMessage(ctx context.Context, m *Msg) bool {
	logger := v.WithContext(ctx)

	if m == nil {
		logger.Warning("syntax validation failed: m is nil")
		return false
	}

	if m.PubKey == nil {
		logger.Warning("syntax validation failed: missing public key")
		return false
	}

	if m.InnerMsg == nil {
		logger.With().Warning("syntax validation failed: inner message is nil",
			log.String("sender_id", m.PubKey.ShortString()))
		return false
	}

	if m.InnerMsg.Values == nil {
		logger.With().Warning("syntax validation failed: set is nil",
			log.String("sender_id", m.PubKey.ShortString()),
			types.LayerID(m.InnerMsg.InstanceID),
			log.String("msg_type", m.InnerMsg.Type.String()))
		return false
	}

	if len(m.InnerMsg.Values) == 0 {
		logger.With().Warning("syntax validation failed: set is empty",
			log.String("sender_id", m.PubKey.ShortString()),
			types.LayerID(m.InnerMsg.InstanceID),
			log.String("msg_type", m.InnerMsg.Type.String()))
		return false
	}

	claimedRound := m.InnerMsg.K % 4
	switch m.InnerMsg.Type {
	case pre:
		return true
	case status:
		return claimedRound == statusRound
	case proposal:
		return claimedRound == proposalRound && v.validateSVP(ctx, m)
	case commit:
		return claimedRound == commitRound
	case notify:
		return v.validateCertificate(ctx, m.InnerMsg.Cert)
	default:
		logger.With().Error("unknown message type encountered during syntactic validation",
			log.String("msg_type", m.InnerMsg.Type.String()))
		return false
	}
}

var (
	errNilValidators     = errors.New("validators param is nil")
	errNilAggMsgs        = errors.New("aggMsg is nil")
	errNilMsgsSlice      = errors.New("messages slice is nil")
	errMsgsCountMismatch = errors.New("number of messages does not match the threshold")
	errDupSender         = errors.New("duplicate sender detected")
	errInnerSyntax       = errors.New("invalid syntax for inner message")
	errInnerEligibility  = errors.New("inner message is not eligible")
	errInnerFunc         = errors.New("inner message did not pass validation function")
)

// validate the provided aggregated messages by the provided validators.
func (v *syntaxContextValidator) validateAggregatedMessage(ctx context.Context, aggMsg *aggregatedMessages, validators []func(m *Msg) bool) error {
	if validators == nil {
		return errNilValidators
	}

	if aggMsg == nil {
		return errNilAggMsgs
	}

	if aggMsg.Messages == nil { // must contain status Messages
		return errNilMsgsSlice
	}

	var count int
	for _, m := range aggMsg.Messages {
		count += int(m.InnerMsg.EligibilityCount)
	}

	if count < v.threshold { // must fit eligibility threshold
		v.WithContext(ctx).With().Warning("aggregated validation failed: total eligibility of messages does not match",
			log.Int("expected", v.threshold),
			log.Int("actual", len(aggMsg.Messages)))
		return errMsgsCountMismatch
	}

	senders := make(map[string]struct{})
	for _, innerMsg := range aggMsg.Messages {
		// check if exist in cache of valid messages
		if pub := v.validMsgsTracker.PublicKey(innerMsg); pub != nil {
			// validate unique sender
			if _, exist := senders[pub.String()]; exist { // pub already exist
				return errDupSender
			}
			senders[pub.String()] = struct{}{} // mark sender as exist

			// passed validation, continue to next message
			continue
		}

		// extract public key
		var iMsg, err = newMsg(ctx, innerMsg, v.stateQuerier)
		if err != nil {
			return err
		}

		pub := iMsg.PubKey
		// validate unique sender
		if _, exist := senders[pub.String()]; exist { // pub already exist
			return errDupSender
		}
		senders[pub.String()] = struct{}{} // mark sender as exist

		if !v.SyntacticallyValidateMessage(ctx, iMsg) {
			return errInnerSyntax
		}

		// validate role
		if !v.roleValidator.Validate(ctx, iMsg) {
			return errInnerEligibility
		}

		// validate with attached validators
		for _, vFunc := range validators {
			if !vFunc(iMsg) {
				return errInnerFunc
			}
		}

		// the message is valid, track it
		v.validMsgsTracker.Track(iMsg)
	}

	return nil
}

func (v *syntaxContextValidator) validateSVP(ctx context.Context, msg *Msg) bool {
	logger := v.WithContext(ctx)

	defer func(startTime time.Time) {
		logger.With().Debug("svp validation duration",
			log.String("duration", time.Now().Sub(startTime).String()))
	}(time.Now())
	proposalIter := iterationFromCounter(msg.InnerMsg.K)
	validateSameIteration := func(m *Msg) bool {
		statusIter := iterationFromCounter(m.InnerMsg.K)
		if proposalIter != statusIter { // not same iteration
			logger.With().Warning("proposal validation failed: not same iteration",
				log.String("sender_id", m.PubKey.ShortString()),
				types.LayerID(m.InnerMsg.InstanceID),
				log.Int32("expected", proposalIter),
				log.Int32("actual", statusIter))
			return false
		}

		return true
	}
	logger = logger.WithFields(
		log.String("sender_id", msg.PubKey.ShortString()),
		types.LayerID(msg.InnerMsg.InstanceID),
	)

	validators := []func(m *Msg) bool{validateStatusType, validateSameIteration, v.statusValidator}
	if err := v.validateAggregatedMessage(ctx, msg.InnerMsg.Svp, validators); err != nil {
		logger.With().Warning("proposal validation failed: failed to validate aggregated messages",
			log.Err(err))
		return false
	}

	maxKi := int32(-1) // Ki>=-1
	var maxSet []types.BlockID
	for _, status := range msg.InnerMsg.Svp.Messages {
		// track max
		if status.InnerMsg.Ki > maxKi {
			maxKi = status.InnerMsg.Ki
			maxSet = status.InnerMsg.Values
		}
	}

	if maxKi == -1 { // type A
		if !v.validateSVPTypeA(ctx, msg) {
			logger.Warning("proposal validation failed: type a validation failed")
			return false
		}
	} else {
		if !v.validateSVPTypeB(ctx, msg, NewSet(maxSet)) { // type B
			logger.Warning("proposal validation failed: type b validation failed")
			return false
		}
	}

	return true
}

func (v *syntaxContextValidator) validateCertificate(ctx context.Context, cert *certificate) bool {
	logger := v.WithContext(ctx)

	defer func(startTime time.Time) {
		logger.With().Debug("certificate validation duration",
			log.String("duration", time.Now().Sub(startTime).String()))
	}(time.Now())

	if cert == nil {
		logger.Warning("certificate validation failed: certificate is nil")
		return false
	}

	// loggererify agg msgs
	if cert.AggMsgs == nil {
		logger.Warning("certificate validation failed: AggMsgs is nil")
		return false
	}

	// refill Values
	for _, commit := range cert.AggMsgs.Messages {
		if commit.InnerMsg == nil {
			logger.Warning("certificate validation failed: inner commit message is nil")
			return false
		}

		commit.InnerMsg.Values = cert.Values
	}

	// Note: no need to validate notify.Values=commits.Values because we refill the InnerMsg with notify.Values
	validateSameK := func(m *Msg) bool { return m.InnerMsg.K == cert.AggMsgs.Messages[0].InnerMsg.K }
	validators := []func(m *Msg) bool{validateCommitType, validateSameK}
	if err := v.validateAggregatedMessage(ctx, cert.AggMsgs, validators); err != nil {
		logger.With().Warning("Certificate validation failed: aggregated messages validation failed", log.Err(err))
		return false
	}

	return true
}

func validateCommitType(m *Msg) bool {
	return messageType(m.InnerMsg.Type) == commit
}

func validateStatusType(m *Msg) bool {
	return messageType(m.InnerMsg.Type) == status
}

// validate SVP for type A (where all Ki=-1)
func (v *syntaxContextValidator) validateSVPTypeA(ctx context.Context, m *Msg) bool {
	s := NewSet(m.InnerMsg.Values)
	unionSet := NewEmptySet(len(m.InnerMsg.Values))
	for _, status := range m.InnerMsg.Svp.Messages {
		statusSet := NewSet(status.InnerMsg.Values)
		// build union
		for bid := range statusSet.values {
			unionSet.Add(bid) // assuming add is unique
		}
	}

	if !unionSet.Equals(s) { // s should be the union of all statuses
		v.WithContext(ctx).With().Warning("proposal type a validation failed: not a union",
			log.String("expected", s.String()),
			log.String("actual", unionSet.String()))
		return false
	}

	return true
}

// validate SVP for type B (where exist Ki>=0)
func (v *syntaxContextValidator) validateSVPTypeB(ctx context.Context, msg *Msg, maxSet *Set) bool {
	// max set should be equal to the claimed set
	s := NewSet(msg.InnerMsg.Values)
	if !s.Equals(maxSet) {
		v.WithContext(ctx).With().Warning("proposal type b validation failed: max set not equal to proposed set",
			log.String("expected", s.String()),
			log.String("actual", maxSet.String()))
		return false
	}

	return true
}
