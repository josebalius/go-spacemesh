package discovery

import (
	"bytes"
	"context"
	"errors"
	"net"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

func (p *protocol) newPingRequestHandler() func(context.Context, server.Message) ([]byte, error) {
	return func(ctx context.Context, msg server.Message) ([]byte, error) {
		plogger := p.logger.WithContext(ctx).WithFields(log.String("type", "ping"),
			log.String("from", msg.Sender().String()))
		plogger.Debug("handle request")
		pinged := &node.Info{}
		err := types.BytesToInterface(msg.Bytes(), pinged)
		if err != nil {
			plogger.With().Error("failed to deserialize ping message", log.Err(err))
			return nil, server.ErrBadRequest
		}

		if err := p.verifyPinger(msg.Metadata().FromAddress, pinged); err != nil {
			plogger.With().Error("msg contents were not valid", log.Err(err))
			return nil, server.ErrBadRequest
		}

		//pong
		payload, err := types.InterfaceToBytes(p.local)
		// TODO: include the resolved To address
		if err != nil {
			plogger.With().Panic("error marshaling response message (ping)", log.Err(err))
		}

		plogger.Debug("sending pong message")
		return payload, nil
	}
}

func (p *protocol) verifyPinger(from net.Addr, pi *node.Info) error {
	// todo : Validate ToAddr or drop it.
	// todo: check the address provided with an extra ping before updating. ( if we haven't checked it for a while )
	// todo: decide on best way to know our ext address

	if err := pi.Valid(); err != nil {
		return err
	}

	//TODO: only accept local (unspecified/loopback) IPs from other local ips.
	ipfrom, _, _ := net.SplitHostPort(from.String())
	pi.IP = net.ParseIP(ipfrom)

	// inbound ping is the actual source of this node info
	p.table.AddAddress(pi, pi)
	return nil
}

// Ping notifies `peer` about our p2p identity.
func (p *protocol) Ping(ctx context.Context, peer p2pcrypto.PublicKey) error {
	plogger := p.logger.WithFields(log.String("type", "ping"), log.String("to", peer.String()))
	plogger.Debug("send ping request")

	data, err := types.InterfaceToBytes(p.local)
	if err != nil {
		return err
	}
	ch := make(chan []byte, 1)
	foo := func(msg []byte) {
		plogger.Debug("handle ping response")
		sender := &node.Info{}
		err := types.BytesToInterface(msg, sender)

		if err != nil {
			plogger.With().Warning("got unreadable pong", log.Err(err))
			return
		}
		// TODO: if we pinged it we already have id so no need to update,
		//   but what if id or listen address has changed?
		ch <- sender.ID.Bytes()
	}

	err = p.msgServer.SendRequest(ctx, server.PingPong, data, peer, foo, func(err error) {})

	if err != nil {
		return err
	}

	timeout := time.NewTimer(MessageTimeout) // todo: check whether this is useless because of `requestLifetime`
	select {
	case id := <-ch:
		if id == nil {
			return errors.New("failed sending message")
		}
		if !bytes.Equal(id, peer.Bytes()) {
			return errors.New("got pong with different public key")
		}
	case <-timeout.C:
		return errors.New("ping timeout")
	}

	return nil
}
