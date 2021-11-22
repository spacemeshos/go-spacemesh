package hare

// certifyTracker tracks certify messages.
// It also provides the number of certifications tracked for a layer.
type certifyTracker struct {
	certifiers       map[string]struct{} // tracks PubKey->Certifier weight
	minEligibleCount uint32
	totalCount       uint32
}

func newCertifyTracker(minEligibleCount int) *certifyTracker {
	ct := &certifyTracker{}
	ct.certifiers = make(map[string]struct{})
	ct.minEligibleCount = uint32(minEligibleCount)
	return ct
}

// OnCertify tracks the provided notification message.
// Returns true if tracked enough certification messages.
func (ct *certifyTracker) OnCertify(msg *Msg) bool {
	pub := msg.PubKey
	if _, exist := ct.certifiers[pub.String()]; !exist { // already seenSenders
		ct.certifiers[pub.String()] = struct{}{}
		ct.totalCount += uint32(msg.InnerMsg.EligibilityCount)
	}
	return ct.IsCertified()
}

// IsCertified returns true if tracked enough certification messages.
func (ct *certifyTracker) IsCertified() bool {
	return ct.totalCount >= ct.minEligibleCount
}
