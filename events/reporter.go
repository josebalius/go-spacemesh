package events

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// reporter is the event reporter singleton.
var reporter *EventReporter

// ReportNewTx dispatches incoming events to the reporter singleton
func ReportNewTx(tx *types.Transaction) {
	if reporter != nil {
		select {
		case reporter.channelTransaction <- tx:
			log.Info("reported tx on channelTransaction")
		default:
			log.Info("not reporting tx as no one is listening on channelTransaction")
		}
	}

	Publish(NewTx{
		ID:          tx.ID().String(),
		Origin:      tx.Origin().String(),
		Destination: tx.Recipient.String(),
		Amount:      tx.Amount,
		Fee:         tx.Fee,
	})
}

// ReportValidTx reports a valid transaction
func ReportValidTx(tx *types.Transaction, valid bool) {
	Publish(ValidTx{ID: tx.ID().String(), Valid: valid})
}

// ReportNewActivation reports a new activation
func ReportNewActivation(activation *types.ActivationTx) {
	if reporter != nil {
		reporter.channelActivation <- activation
	}
	Publish(NewAtx{
		ID:      activation.ShortString(),
		LayerID: uint64(activation.PubLayerID.GetEpoch()),
	})
}

// ReportRewardReceived reports a new reward
func ReportRewardReceived(account *types.Address, reward uint64) {
	Publish(RewardReceived{
		Coinbase: account.String(),
		Amount:   reward,
	})
}

// ReportNewBlock reports a new block
func ReportNewBlock(blk *types.Block) {
	Publish(NewBlock{
		ID:    blk.ID().String(),
		Atx:   blk.ATXID.ShortString(),
		Layer: uint64(blk.LayerIndex),
	})
}

// ReportValidBlock reports a valid block
func ReportValidBlock(blockID types.BlockID, valid bool) {
	Publish(ValidBlock{
		ID:    blockID.String(),
		Valid: valid,
	})
}

// ReportAtxCreated reports a created activation
func ReportAtxCreated(created bool, layer uint64, id string) {
	Publish(AtxCreated{Created: created, Layer: layer, ID: id})
}

// ReportValidActivation reports a valid activation
func ReportValidActivation(activation *types.ActivationTx, valid bool) {
	Publish(ValidAtx{ID: activation.ShortString(), Valid: valid})
}

// ReportDoneCreatingBlock reports a created block
func ReportDoneCreatingBlock(eligible bool, layer uint64, error string) {
	Publish(DoneCreatingBlock{
		Eligible: eligible,
		Layer:    layer,
		Error:    error,
	})
}

// ReportNewLayer reports a new layer
func ReportNewLayer(layer *types.Layer) {
	if reporter != nil {
		reporter.channelLayer <- layer
	}
}

// ReportError reports an error
func ReportError(err NodeError) {
	if reporter != nil {
		reporter.channelError <- err
	}
}

// ReportNodeStatus reports an update to the node status
func ReportNodeStatus(setters ...SetStatusElem) {
	if reporter != nil {
		for _, setter := range setters {
			setter(&reporter.lastStatus)
		}
		reporter.channelStatus <- reporter.lastStatus
	}
}

// GetNewTxChannel returns a channel of new transactions
func GetNewTxChannel() chan *types.Transaction {
	if reporter != nil {
		return reporter.channelTransaction
	}
	return nil
}

// GetActivationsChannel returns a channel of activations
func GetActivationsChannel() chan *types.ActivationTx {
	if reporter != nil {
		return reporter.channelActivation
	}
	return nil
}

// GetLayerChannel returns a channel of all layer data
func GetLayerChannel() chan *types.Layer {
	if reporter != nil {
		return reporter.channelLayer
	}
	return nil
}

// GetErrorChannel returns a channel for node errors
func GetErrorChannel() chan NodeError {
	if reporter != nil {
		return reporter.channelError
	}
	return nil
}

// GetStatusChannel returns a channel for node status messages
func GetStatusChannel() chan NodeStatus {
	if reporter != nil {
		return reporter.channelStatus
	}
	return nil
}

// InitializeEventReporter initializes the event reporting interface
func InitializeEventReporter(url string) {
	reporter = newEventReporter()
	if url != "" {
		InitializeEventPubsub(url)
	}
}

const (
	NodeErrorTypeError = iota
	NodeErrorTypePanic
	NodeErrorTypePanicSync
	NodeErrorTypePanicP2P
	NodeErrorTypePanicHare
	NodeErrorTypeSignalShutdown
)

// NodeError represents an internal error to be reported
type NodeError struct {
	Msg   string
	Trace string
	Type  int
}

// NodeStatus represents the current status of the node, to be reported
type NodeStatus struct {
	NumPeers      int
	IsSynced      bool
	LayerSynced   types.LayerID
	LayerCurrent  types.LayerID
	LayerVerified types.LayerID
}

type SetStatusElem func(*NodeStatus)

func NumPeers(n int) SetStatusElem {
	return func(ns *NodeStatus) {
		ns.NumPeers = n
	}
}

func IsSynced(synced bool) SetStatusElem {
	return func(ns *NodeStatus) {
		ns.IsSynced = synced
	}
}

func LayerSynced(lid types.LayerID) SetStatusElem {
	return func(ns *NodeStatus) {
		ns.LayerSynced = lid
	}
}

func LayerCurrent(lid types.LayerID) SetStatusElem {
	return func(ns *NodeStatus) {
		ns.LayerCurrent = lid
	}
}

func LayerVerified(lid types.LayerID) SetStatusElem {
	return func(ns *NodeStatus) {
		ns.LayerVerified = lid
	}
}

// EventReporter is the struct that receives incoming events and dispatches them
type EventReporter struct {
	channelTransaction chan *types.Transaction
	channelActivation  chan *types.ActivationTx
	channelLayer       chan *types.Layer
	channelError       chan NodeError
	channelStatus      chan NodeStatus
	lastStatus         NodeStatus
}

func newEventReporter() *EventReporter {
	return &EventReporter{
		channelTransaction: make(chan *types.Transaction),
		channelActivation:  make(chan *types.ActivationTx),
		channelLayer:       make(chan *types.Layer),
		channelError:       make(chan NodeError),
		channelStatus:      make(chan NodeStatus),
		lastStatus:         NodeStatus{},
	}
}

// CloseEventReporter shuts down the event reporting service and closes open channels
func CloseEventReporter() {
	if reporter != nil {
		close(reporter.channelTransaction)
		close(reporter.channelActivation)
		close(reporter.channelLayer)
		close(reporter.channelError)
		close(reporter.channelStatus)
		reporter = nil
	}
}
