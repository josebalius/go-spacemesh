package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const protocol = "/protocol/test/1.0/"

func TestProtocol_ResponseNoDataNoError(t *testing.T) {
	data, err := SerializeResponse(nil, nil)
	assert.Nil(t, err)
	assert.Greater(t, len(data), 0)
	resp, err := deserializeResponse(data)
	assert.NoError(t, err)
	assert.Nil(t, resp.Data)
	assert.NoError(t, resp.getError())
}

func TestProtocol_ResponseNoError(t *testing.T) {
	bts := []byte("Baa Ram Ewe")
	data, err := SerializeResponse(bts, nil)
	assert.Nil(t, err)
	assert.Greater(t, len(data), 0)
	resp, err := deserializeResponse(data)
	assert.NoError(t, err)
	assert.Equal(t, bts, resp.Data)
	assert.Equal(t, nil, resp.getError())
}

func TestProtocol_ResponseHasError(t *testing.T) {
	data, err := SerializeResponse(nil, ErrShuttingDown)
	assert.Nil(t, err)
	assert.Greater(t, len(data), 0)
	resp, err := deserializeResponse(data)
	assert.NoError(t, err)
	assert.Nil(t, resp.Data)
	assert.Equal(t, ErrShuttingDown, resp.getError())
}

func TestProtocol_SendRequest(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	serv1 := NewMsgServer(n1, protocol, 5*time.Second, make(chan service.DirectMessage, config.Values.BufferSize), logtest.New(t).WithName("serv1"))

	mockData := "some value to return"
	handler := func(ctx context.Context, msg []byte) ([]byte, error) {
		return []byte(mockData), nil
	}
	// todo test nonbyte handlers
	serv1.RegisterBytesMsgHandler(1, handler)
	go serv1.Start(context.TODO())

	n2 := sim.NewNode()
	serv2 := NewMsgServer(n2, protocol, 5*time.Second, make(chan service.DirectMessage, config.Values.BufferSize), logtest.New(t).WithName("serv2"))
	go serv2.Start(context.TODO())

	//send request with handler that converts to string and sends via channel
	respCh := make(chan []byte)
	callback := func(resp []byte) {
		respCh <- resp
	}
	errCh := make(chan error)
	errorHandler := func(err error) {
		errCh <- err
	}
	err := serv2.SendRequest(context.TODO(), 1, nil, n1.PublicKey(), callback, errorHandler)
	require.NoError(t, err, "Should not return error")
	resp := <-respCh

	assert.EqualValues(t, mockData, resp, "value received did not match correct value")
	assert.Empty(t, errCh, "should not receive error from peer")

	// Now try sending to a bad address
	randkey := p2pcrypto.NewRandomPubkey()
	err = serv2.SendRequest(context.TODO(), 1, nil, randkey, callback, func(err error) {})
	assert.Error(t, err, "Sending to bad address should return error")
}

func TestProtocol_SendRequestPeerReturnError(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	srv1 := NewMsgServer(n1, protocol, 5*time.Second, make(chan service.DirectMessage, config.Values.BufferSize), logtest.New(t).WithName("serv1"))

	// handler returns error
	handler := func(ctx context.Context, msg []byte) ([]byte, error) {
		return nil, ErrBadRequest
	}
	srv1.RegisterBytesMsgHandler(1, handler)
	go srv1.Start(context.TODO())

	n2 := sim.NewNode()
	srv2 := NewMsgServer(n2, protocol, 5*time.Second, make(chan service.DirectMessage, config.Values.BufferSize), logtest.New(t).WithName("serv2"))
	go srv2.Start(context.TODO())

	respCh := make(chan []byte)
	callback := func(resp []byte) {
		respCh <- resp
	}
	errCh := make(chan error)
	errorHandler := func(err error) {
		errCh <- err
	}
	err := srv2.SendRequest(context.TODO(), 1, nil, n1.PublicKey(), callback, errorHandler)
	require.NoError(t, err)

	peerErr := <-errCh
	assert.Empty(t, respCh, "value received did not match correct value")
	assert.Equal(t, ErrBadRequest, peerErr, "Should return error")
}

func TestProtocol_CleanOldPendingMessages(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	serv1 := NewMsgServer(n1, protocol, 5*time.Second, make(chan service.DirectMessage, config.Values.BufferSize), logtest.New(t).WithName("serv1"))
	go serv1.Start(context.TODO())
	//handler that returns some bytes on request

	handler := func(ctx context.Context, msg []byte) ([]byte, error) {
		time.Sleep(2 * time.Second)
		return nil, nil
	}

	serv1.RegisterBytesMsgHandler(1, handler)

	n2 := sim.NewNode()
	serv2 := NewMsgServer(n2, protocol, 10*time.Millisecond, make(chan service.DirectMessage, config.Values.BufferSize), logtest.New(t).WithName("serv2"))
	go serv2.Start(context.TODO())

	//send request with handler that converts to string and sends via channel
	respCh := make(chan []byte)
	callback := func(resp []byte) {
		respCh <- resp
	}

	err := serv2.SendRequest(context.TODO(), 1, nil, n1.PublicKey(), callback, func(err error) {})
	assert.NoError(t, err, "Should not return error")
	assert.EqualValues(t, 1, serv2.pendingQueue.Len(), "value received did not match correct value1")

	timeout := time.After(3 * time.Second)
	// Keep trying until we're timed out or got a result or got an error

	select {
	// Got a timeout! fail with a timeout error
	case <-timeout:
		t.Error("timeout")
		return
	default:
		if serv2.pendingQueue.Len() == 0 {
			assert.EqualValues(t, 0, serv2.pendingQueue.Len(), "value received did not match correct value2")
		}
	}
}

func TestProtocol_StartContextCancellation(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	serv1 := NewMsgServer(n1, protocol, 5*time.Second, make(chan service.DirectMessage, config.Values.BufferSize), logtest.New(t).WithName("serv1"))

	ctx, _ := context.WithTimeout(context.Background(), 50*time.Millisecond)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		err := serv1.Start(ctx)
		if err != context.DeadlineExceeded {
			t.Errorf("expected deadline exceeded error, got: %v", err)
		}
	}()

	wg.Wait()
}

func TestProtocol_StartClosedIngressFinish(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	ingressCh := make(chan service.DirectMessage, config.Values.BufferSize)
	serv1 := NewMsgServer(n1, protocol, 5*time.Second, ingressCh, logtest.New(t).WithName("serv1"))

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		err := serv1.Start(context.TODO())
		if err != context.Canceled {
			t.Errorf("expected context canceled error, got: %v", err)
		}
	}()

	ticker := time.Tick(50 * time.Millisecond)

	<-ticker
	close(ingressCh)

	wg.Wait()
}
