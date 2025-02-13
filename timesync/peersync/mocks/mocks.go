// Code generated by MockGen. DO NOT EDIT.
// Source: ./sync.go

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"
	time "time"

	gomock "github.com/golang/mock/gomock"
	p2pcrypto "github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	service "github.com/spacemeshos/go-spacemesh/p2p/service"
)

// MockTime is a mock of Time interface.
type MockTime struct {
	ctrl     *gomock.Controller
	recorder *MockTimeMockRecorder
}

// MockTimeMockRecorder is the mock recorder for MockTime.
type MockTimeMockRecorder struct {
	mock *MockTime
}

// NewMockTime creates a new mock instance.
func NewMockTime(ctrl *gomock.Controller) *MockTime {
	mock := &MockTime{ctrl: ctrl}
	mock.recorder = &MockTimeMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockTime) EXPECT() *MockTimeMockRecorder {
	return m.recorder
}

// Now mocks base method.
func (m *MockTime) Now() time.Time {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Now")
	ret0, _ := ret[0].(time.Time)
	return ret0
}

// Now indicates an expected call of Now.
func (mr *MockTimeMockRecorder) Now() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Now", reflect.TypeOf((*MockTime)(nil).Now))
}

// MockNetwork is a mock of Network interface.
type MockNetwork struct {
	ctrl     *gomock.Controller
	recorder *MockNetworkMockRecorder
}

// MockNetworkMockRecorder is the mock recorder for MockNetwork.
type MockNetworkMockRecorder struct {
	mock *MockNetwork
}

// NewMockNetwork creates a new mock instance.
func NewMockNetwork(ctrl *gomock.Controller) *MockNetwork {
	mock := &MockNetwork{ctrl: ctrl}
	mock.recorder = &MockNetworkMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockNetwork) EXPECT() *MockNetworkMockRecorder {
	return m.recorder
}

// RegisterDirectProtocolWithChannel mocks base method.
func (m *MockNetwork) RegisterDirectProtocolWithChannel(protocol string, ingressChannel chan service.DirectMessage) chan service.DirectMessage {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RegisterDirectProtocolWithChannel", protocol, ingressChannel)
	ret0, _ := ret[0].(chan service.DirectMessage)
	return ret0
}

// RegisterDirectProtocolWithChannel indicates an expected call of RegisterDirectProtocolWithChannel.
func (mr *MockNetworkMockRecorder) RegisterDirectProtocolWithChannel(protocol, ingressChannel interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegisterDirectProtocolWithChannel", reflect.TypeOf((*MockNetwork)(nil).RegisterDirectProtocolWithChannel), protocol, ingressChannel)
}

// SendWrappedMessage mocks base method.
func (m *MockNetwork) SendWrappedMessage(ctx context.Context, nodeID p2pcrypto.PublicKey, protocol string, payload *service.DataMsgWrapper) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SendWrappedMessage", ctx, nodeID, protocol, payload)
	ret0, _ := ret[0].(error)
	return ret0
}

// SendWrappedMessage indicates an expected call of SendWrappedMessage.
func (mr *MockNetworkMockRecorder) SendWrappedMessage(ctx, nodeID, protocol, payload interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SendWrappedMessage", reflect.TypeOf((*MockNetwork)(nil).SendWrappedMessage), ctx, nodeID, protocol, payload)
}

// SubscribePeerEvents mocks base method.
func (m *MockNetwork) SubscribePeerEvents() (chan p2pcrypto.PublicKey, chan p2pcrypto.PublicKey) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SubscribePeerEvents")
	ret0, _ := ret[0].(chan p2pcrypto.PublicKey)
	ret1, _ := ret[1].(chan p2pcrypto.PublicKey)
	return ret0, ret1
}

// SubscribePeerEvents indicates an expected call of SubscribePeerEvents.
func (mr *MockNetworkMockRecorder) SubscribePeerEvents() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SubscribePeerEvents", reflect.TypeOf((*MockNetwork)(nil).SubscribePeerEvents))
}
