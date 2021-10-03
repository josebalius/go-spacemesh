// Package server is used to wrap the p2p services to define multiple req-res messages under one protocol.
package server

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"golang.org/x/sync/errgroup"
)

// MessageType is a uint32 used to distinguish between server messages inside a single protocol.
type MessageType uint32

const (
	// PingPong is the ping protocol ID
	PingPong MessageType = iota
	// GetAddresses is the findnode protocol ID
	GetAddresses
	// LayerBlocksMsg is used to fetch block IDs for a given layer hash
	LayerBlocksMsg
	// AtxIDsMsg is used to fetch ATXs for a given epoch
	AtxIDsMsg
	// TortoiseBeaconMsg is used to fetch tortoise beacon messages for a given epoch
	TortoiseBeaconMsg
	// Fetch is used to fetch data for a given hash
	Fetch
	// RequestTimeSync is used for time synchronization with peers.
	RequestTimeSync
)

// Message is helper type for `MessageServer` messages.
type Message interface {
	service.DirectMessage
	Data() service.Data
}

// requestSnapshot is a queue entry used to track the lifetime of a request in the server.
// These entries are meant to be processed in a timely manner and are evicted from the queue
// after a certain time passes.
type requestSnapshot struct {
	id        uint64
	timestamp time.Time
}

type responseHandlers struct {
	okCallback   func(msg []byte)
	failCallBack func(err error)
}

// ErrShuttingDown is returned to the peer when the node is shutting down.
var ErrShuttingDown = errors.New("node is shutting down")

// ErrBadRequest is returned to the peer upon failure to parse the request.
var ErrBadRequest = errors.New("unable to parse request")

// ErrRequestTimeout is returned to the caller when the request times out.
var ErrRequestTimeout = errors.New("request timed out")

// MessageServer is a request-response multiplexer on top of the p2p layer. it provides a way to register
// message types on top of a protocol and declare request and response handlers. it matches incoming responses to requests.
type MessageServer struct {
	// ReqID must be declared first to ensure 8 byte alignment on 32-bit systems, required by atomic operations)
	ReqID uint64

	name    string // server name
	network Service

	pendMutex    sync.RWMutex                // guards pendingQueue, resHandlers, reqElements
	pendingQueue *list.List                  // queue of pending messages
	resHandlers  map[uint64]responseHandlers // response handlers by request ReqID
	reqElements  map[uint64]*list.Element    // queue elements by request ID

	msgRequestHandlersMu sync.RWMutex                                                   // guards msgRequestHandlers
	msgRequestHandlers   map[MessageType]func(context.Context, Message) ([]byte, error) // request handlers by request type

	ingressChannel  chan service.DirectMessage // chan to relay messages into the server
	requestLifetime time.Duration
	logger          log.Log
}

// Service is the subset of method used by MessageServer for p2p communications.
type Service interface {
	RegisterDirectProtocolWithChannel(protocol string, ingressChannel chan service.DirectMessage) chan service.DirectMessage
	SendWrappedMessage(ctx context.Context, nodeID p2pcrypto.PublicKey, protocol string, payload *service.DataMsgWrapper) error
}

// NewMsgServer registers a protocol and returns a new server to declare request and response handlers on.
func NewMsgServer(network Service, name string, requestLifetime time.Duration, c chan service.DirectMessage, logger log.Log) *MessageServer {
	return &MessageServer{
		logger:             logger,
		name:               name,
		resHandlers:        make(map[uint64]responseHandlers),
		reqElements:        make(map[uint64]*list.Element),
		pendingQueue:       list.New(),
		network:            network,
		ingressChannel:     network.RegisterDirectProtocolWithChannel(name, c),
		msgRequestHandlers: make(map[MessageType]func(context.Context, Message) ([]byte, error)),
		requestLifetime:    requestLifetime,
	}
}

// Start will start the server by creating a number of workers that are listening on the
// ingressChannel for incoming messages. Each message is processed concurrently up to the max
// number of workers.
//
// An additional concurrent process is started to clean up stale messages.
// The server can be stopped via context cancellation or closing the ingressChannel.
func (p *MessageServer) Start(ctx context.Context) error {
	// Initiate logger
	ctx = log.WithNewSessionID(ctx)
	defer p.logger.With().Info("shutting down protocol", log.String("protocol", p.name))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // make sure we clean up resources

	g, ctx := errgroup.WithContext(ctx)
	for i := 0; i < runtime.NumCPU(); i++ {
		g.Go(func() error {
			return p.processIngressMessages(ctx)
		})
	}

	done := make(chan error, 2)

	// Clean up stale messages concurrently. It will stop once the context is cancelled.
	go func() {
		done <- p.cleanStaleMessages(ctx)
	}()

	// Wait for all workers to finish
	go func() {
		done <- g.Wait()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// processIngressMessages drains the ingressChannel until the context is cancelled or the channel is closed.
func (p *MessageServer) processIngressMessages(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-p.ingressChannel:
			// generate new reqID for message
			ctx = log.WithNewRequestID(ctx)
			p.logger.WithContext(ctx).Debug("new msg received from channel")
			if !ok {
				// channel is closed
				p.logger.WithContext(ctx).Error("read loop was closed")
				return nil
			}

			m, ok := msg.(Message)
			if !ok {
				return fmt.Errorf("invalid message type, expected Message, received: %T", msg)
			}

			if err := p.handleMessage(ctx, m); err != nil {
				return fmt.Errorf("failed to handle message: %w", err)
			}
		}
	}
}

// cleanStaleMessages ticks some time after the requestLifetime config and looks at the
// front of the pending queue for request snapshots that are past the max lifetime.
// If a snapshot is found, the element is queued for removal from the queue and the
// request handler is notified via its fail callback. It returns when the context is cancelled.
func (p *MessageServer) cleanStaleMessages(ctx context.Context) error {
	ticker := time.NewTicker(p.requestLifetime * 100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			p.pendMutex.RLock()
			p.logger.With().Debug(
				"checking for stale message in MessageServer queue",
				log.Int("queue_length", p.pendingQueue.Len()),
			)
			pendingMessage := p.pendingQueue.Front()
			p.pendMutex.RUnlock()

			if pendingMessage == nil {
				p.logger.Debug("cleanStaleMessages queue empty")
				continue // TODO(josebalius): why were we returning?
			}

			rs, ok := pendingMessage.Value.(requestSnapshot)
			if !ok {
				return fmt.Errorf("invalid pending message type, expected requestSnapshot, got: %T", pendingMessage.Value)
			}

			if time.Since(rs.timestamp) < p.requestLifetime {
				p.logger.Debug("cleanStaleMessages no more stale messages")
				continue // TODO(josebalius): why were we returning?
			}

			p.logger.With().Debug("cleanStaleMessages remove request", log.Uint64("id", rs.id))
			p.pendMutex.RLock()
			resHandler, ok := p.resHandlers[rs.id]
			p.pendMutex.RUnlock()

			if ok {
				resHandler.failCallBack(ErrRequestTimeout)
			}

			p.removeFromPending(rs.id, pendingMessage)
		}
	}
}

// removeFromPending removes the given request ID and element from both the pending queue
// and handlers that have been set for that request ID.
func (p *MessageServer) removeFromPending(reqID uint64, reqElement *list.Element) {
	p.pendMutex.Lock()

	if reqElement != nil {
		p.pendingQueue.Remove(reqElement)
	}

	p.logger.With().Debug("removed request", log.Uint64("p2p_request_id", reqID))
	delete(p.resHandlers, reqID)
	delete(p.reqElements, reqID)
	p.logger.With().Debug("delete request result handler", log.Uint64("p2p_request_id", reqID))
	p.pendMutex.Unlock()
}

// handleMessage parses the msg into two a request or response data type and calls the respective handler.
func (p *MessageServer) handleMessage(ctx context.Context, msg Message) error {
	data, ok := msg.Data().(*service.DataMsgWrapper)
	if !ok {
		return fmt.Errorf("invalid message data type, expected *service.DataMsgWrapper, got: %T", msg.Data())
	}

	if data.Req {
		p.handleRequestMessage(ctx, msg, data)
	} else {
		p.handleResponseMessage(ctx, data)
	}

	return nil
}

// handleRequestMessage looks up the request handler by request type, invokes it and serializes the response.
// The response is then sent back to the sender.
func (p *MessageServer) handleRequestMessage(ctx context.Context, msg Message, req *service.DataMsgWrapper) {
	logger := p.logger.WithContext(ctx)
	logger.Debug("handleRequestMessage start")
	defer logger.Debug("handleRequestMessage close")

	p.msgRequestHandlersMu.RLock()
	reqHandler, ok := p.msgRequestHandlers[MessageType(req.MsgType)]
	p.msgRequestHandlersMu.RUnlock()

	if !ok {
		logger.With().Error("handler missing for request",
			log.Uint64("p2p_request_id", req.ReqID),
			log.String("protocol", p.name),
			log.Uint32("p2p_msg_type", req.MsgType))
		return
	}

	logger.With().Debug("handle request", log.Uint32("p2p_msg_type", req.MsgType))
	data, err := reqHandler(ctx, msg)
	payload := SerializeResponse(data, err)

	resp := &service.DataMsgWrapper{MsgType: req.MsgType, ReqID: req.ReqID, Payload: payload}
	if sendErr := p.network.SendWrappedMessage(ctx, msg.Sender(), p.name, resp); sendErr != nil {
		logger.With().Error("error sending response message", log.Err(sendErr))
	}
}

// handleResponseMessage finds the associated response handler and processes the response. The request is removed
// from the pending queue. Errors are reported back to the response handler via the failCallback method.
// In the success case, the okCallback is invoked with the response data.
func (p *MessageServer) handleResponseMessage(ctx context.Context, headers *service.DataMsgWrapper) {
	logger := p.logger.WithContext(ctx)
	logger.With().Debug("handleResponseMessage", log.Uint64("p2p_request_id", headers.ReqID))
	defer logger.Debug("handleResponseMessage close")

	// get and remove from pendingMap
	p.pendMutex.RLock()
	resHandler, ok := p.resHandlers[headers.ReqID]
	reqElement := p.reqElements[headers.ReqID]
	p.pendMutex.RUnlock()
	p.removeFromPending(headers.ReqID, reqElement)

	if !ok {
		logger.With().Error("can't find handler", log.Uint64("p2p_request_id", headers.ReqID))
		return
	}

	resp, err := deserializeResponse(headers.Payload)
	if err != nil {
		logger.With().Warning("failed to deserialize response", log.Err(err))
		resHandler.failCallBack(err)
		return
	}

	peerErr := resp.getError()
	if peerErr != nil {
		resHandler.failCallBack(peerErr)
		return
	}

	resHandler.okCallback(resp.Data)
}

// RegisterMsgHandler sets the handler to act on a specific message request.
func (p *MessageServer) RegisterMsgHandler(msgType MessageType, reqHandler func(context.Context, Message) ([]byte, error)) {
	p.msgRequestHandlersMu.Lock()
	p.msgRequestHandlers[msgType] = reqHandler
	p.msgRequestHandlersMu.Unlock()
}

func extractPayload(m Message) ([]byte, error) {
	data, ok := m.Data().(*service.DataMsgWrapper)
	if !ok {
		return nil, errors.New("invalid message type")
	}
	return data.Payload, nil
}

func handlerFromBytesHandler(in func(context.Context, []byte) ([]byte, error)) func(context.Context, Message) ([]byte, error) {
	return func(ctx context.Context, message Message) ([]byte, error) {
		payload, err := extractPayload(message)
		if err != nil {
			return nil, fmt.Errorf("failed to extract payload: %w", err)
		}
		return in(ctx, payload)
	}
}

// RegisterBytesMsgHandler sets the handler to act on a specific message request.
func (p *MessageServer) RegisterBytesMsgHandler(msgType MessageType, reqHandler func(context.Context, []byte) ([]byte, error)) {
	p.RegisterMsgHandler(msgType, handlerFromBytesHandler(reqHandler))
}

// SendRequest sends the request and saves the response handlers along with the request element into the pending queue.
// If the request fails to send, it is immediately removed from the pending queue.
func (p *MessageServer) SendRequest(ctx context.Context, msgType MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), errorHandler func(err error)) error {
	reqID := p.newReqID()

	// Add requestID to context
	ctx = log.WithNewRequestID(ctx,
		log.Uint64("p2p_request_id", reqID),
		log.Uint32("p2p_msg_type", uint32(msgType)),
		log.FieldNamed("recipient", address))

	p.pendMutex.Lock()
	p.resHandlers[reqID] = responseHandlers{resHandler, errorHandler}
	p.reqElements[reqID] = p.pendingQueue.PushBack(requestSnapshot{id: reqID, timestamp: time.Now()})
	p.pendMutex.Unlock()

	msg := &service.DataMsgWrapper{Req: true, ReqID: reqID, MsgType: uint32(msgType), Payload: payload}
	if err := p.network.SendWrappedMessage(ctx, address, p.name, msg); err != nil {
		p.logger.WithContext(ctx).With().Error("sending message failed",
			log.Int("msglen", len(payload)),
			log.Err(err))

		p.removeFromPending(reqID, p.reqElements[reqID])
		return err
	}

	p.logger.WithContext(ctx).Debug("sent request")
	return nil
}

// TODO: make these longer, and random, to make it easier to find them in the logs
// TODO(josebalius): can we make these uuids and drop the atomic functionality?
func (p *MessageServer) newReqID() uint64 {
	return atomic.AddUint64(&p.ReqID, 1)
}
