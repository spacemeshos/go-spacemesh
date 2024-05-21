// Code generated by MockGen. DO NOT EDIT.
// Source: ./handler_v2.go
//
// Generated by this command:
//
//	mockgen -typed -source=./handler_v2.go -destination=mocks_handler_v2.go -package=activation
//

// Package activation is a generated GoMock package.
package activation

import (
	context "context"
	reflect "reflect"

	types "github.com/spacemeshos/go-spacemesh/common/types"
	gomock "go.uber.org/mock/gomock"
)

// MocknipostValidatorV2 is a mock of nipostValidatorV2 interface.
type MocknipostValidatorV2 struct {
	ctrl     *gomock.Controller
	recorder *MocknipostValidatorV2MockRecorder
}

// MocknipostValidatorV2MockRecorder is the mock recorder for MocknipostValidatorV2.
type MocknipostValidatorV2MockRecorder struct {
	mock *MocknipostValidatorV2
}

// NewMocknipostValidatorV2 creates a new mock instance.
func NewMocknipostValidatorV2(ctrl *gomock.Controller) *MocknipostValidatorV2 {
	mock := &MocknipostValidatorV2{ctrl: ctrl}
	mock.recorder = &MocknipostValidatorV2MockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocknipostValidatorV2) EXPECT() *MocknipostValidatorV2MockRecorder {
	return m.recorder
}

// IsVerifyingFullPost mocks base method.
func (m *MocknipostValidatorV2) IsVerifyingFullPost() bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "IsVerifyingFullPost")
	ret0, _ := ret[0].(bool)
	return ret0
}

// IsVerifyingFullPost indicates an expected call of IsVerifyingFullPost.
func (mr *MocknipostValidatorV2MockRecorder) IsVerifyingFullPost() *MocknipostValidatorV2IsVerifyingFullPostCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "IsVerifyingFullPost", reflect.TypeOf((*MocknipostValidatorV2)(nil).IsVerifyingFullPost))
	return &MocknipostValidatorV2IsVerifyingFullPostCall{Call: call}
}

// MocknipostValidatorV2IsVerifyingFullPostCall wrap *gomock.Call
type MocknipostValidatorV2IsVerifyingFullPostCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MocknipostValidatorV2IsVerifyingFullPostCall) Return(arg0 bool) *MocknipostValidatorV2IsVerifyingFullPostCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MocknipostValidatorV2IsVerifyingFullPostCall) Do(f func() bool) *MocknipostValidatorV2IsVerifyingFullPostCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MocknipostValidatorV2IsVerifyingFullPostCall) DoAndReturn(f func() bool) *MocknipostValidatorV2IsVerifyingFullPostCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// PoetMembership mocks base method.
func (m *MocknipostValidatorV2) PoetMembership(ctx context.Context, membership *types.MultiMerkleProof, postChallenge types.Hash32, poetChallenges [][]byte) (uint64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PoetMembership", ctx, membership, postChallenge, poetChallenges)
	ret0, _ := ret[0].(uint64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// PoetMembership indicates an expected call of PoetMembership.
func (mr *MocknipostValidatorV2MockRecorder) PoetMembership(ctx, membership, postChallenge, poetChallenges any) *MocknipostValidatorV2PoetMembershipCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PoetMembership", reflect.TypeOf((*MocknipostValidatorV2)(nil).PoetMembership), ctx, membership, postChallenge, poetChallenges)
	return &MocknipostValidatorV2PoetMembershipCall{Call: call}
}

// MocknipostValidatorV2PoetMembershipCall wrap *gomock.Call
type MocknipostValidatorV2PoetMembershipCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MocknipostValidatorV2PoetMembershipCall) Return(arg0 uint64, arg1 error) *MocknipostValidatorV2PoetMembershipCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MocknipostValidatorV2PoetMembershipCall) Do(f func(context.Context, *types.MultiMerkleProof, types.Hash32, [][]byte) (uint64, error)) *MocknipostValidatorV2PoetMembershipCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MocknipostValidatorV2PoetMembershipCall) DoAndReturn(f func(context.Context, *types.MultiMerkleProof, types.Hash32, [][]byte) (uint64, error)) *MocknipostValidatorV2PoetMembershipCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// PostV2 mocks base method.
func (m *MocknipostValidatorV2) PostV2(ctx context.Context, smesherID types.NodeID, commitment types.ATXID, post *types.Post, challenge []byte, numUnits uint32, opts ...validatorOption) error {
	m.ctrl.T.Helper()
	varargs := []any{ctx, smesherID, commitment, post, challenge, numUnits}
	for _, a := range opts {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "PostV2", varargs...)
	ret0, _ := ret[0].(error)
	return ret0
}

// PostV2 indicates an expected call of PostV2.
func (mr *MocknipostValidatorV2MockRecorder) PostV2(ctx, smesherID, commitment, post, challenge, numUnits any, opts ...any) *MocknipostValidatorV2PostV2Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]any{ctx, smesherID, commitment, post, challenge, numUnits}, opts...)
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PostV2", reflect.TypeOf((*MocknipostValidatorV2)(nil).PostV2), varargs...)
	return &MocknipostValidatorV2PostV2Call{Call: call}
}

// MocknipostValidatorV2PostV2Call wrap *gomock.Call
type MocknipostValidatorV2PostV2Call struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MocknipostValidatorV2PostV2Call) Return(arg0 error) *MocknipostValidatorV2PostV2Call {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MocknipostValidatorV2PostV2Call) Do(f func(context.Context, types.NodeID, types.ATXID, *types.Post, []byte, uint32, ...validatorOption) error) *MocknipostValidatorV2PostV2Call {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MocknipostValidatorV2PostV2Call) DoAndReturn(f func(context.Context, types.NodeID, types.ATXID, *types.Post, []byte, uint32, ...validatorOption) error) *MocknipostValidatorV2PostV2Call {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// VRFNonceV2 mocks base method.
func (m *MocknipostValidatorV2) VRFNonceV2(smesherID types.NodeID, commitment types.ATXID, vrfNonce uint64, numUnits uint32) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "VRFNonceV2", smesherID, commitment, vrfNonce, numUnits)
	ret0, _ := ret[0].(error)
	return ret0
}

// VRFNonceV2 indicates an expected call of VRFNonceV2.
func (mr *MocknipostValidatorV2MockRecorder) VRFNonceV2(smesherID, commitment, vrfNonce, numUnits any) *MocknipostValidatorV2VRFNonceV2Call {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "VRFNonceV2", reflect.TypeOf((*MocknipostValidatorV2)(nil).VRFNonceV2), smesherID, commitment, vrfNonce, numUnits)
	return &MocknipostValidatorV2VRFNonceV2Call{Call: call}
}

// MocknipostValidatorV2VRFNonceV2Call wrap *gomock.Call
type MocknipostValidatorV2VRFNonceV2Call struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MocknipostValidatorV2VRFNonceV2Call) Return(arg0 error) *MocknipostValidatorV2VRFNonceV2Call {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MocknipostValidatorV2VRFNonceV2Call) Do(f func(types.NodeID, types.ATXID, uint64, uint32) error) *MocknipostValidatorV2VRFNonceV2Call {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MocknipostValidatorV2VRFNonceV2Call) DoAndReturn(f func(types.NodeID, types.ATXID, uint64, uint32) error) *MocknipostValidatorV2VRFNonceV2Call {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
