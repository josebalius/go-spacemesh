package eligibility

import (
	"context"
	"encoding/binary"

	lru "github.com/hashicorp/golang-lru"
	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
)

type addGet interface {
	Add(key, value interface{}) (evicted bool)
	Get(key interface{}) (value interface{}, ok bool)
}

// Beacon provides the value that is under consensus as defined by the hare.
type Beacon struct {
	beaconGetter    blocks.BeaconGetter
	confidenceParam uint32
	cache           addGet
	log.Log
}

// NewBeacon returns a new beacon
// confidenceParam is the number of layers that the Beacon assumes for consensus view.
func NewBeacon(beaconGetter blocks.BeaconGetter, confidenceParam uint32, logger log.Log) *Beacon {
	c, err := lru.New(activesCacheSize)
	if err != nil {
		logger.With().Panic("could not create lru cache", log.Err(err))
	}
	return &Beacon{
		beaconGetter:    beaconGetter,
		confidenceParam: confidenceParam,
		cache:           c,
		Log:             logger,
	}
}

// Value returns the beacon value for an epoch
// Note: Value is concurrency-safe but not concurrency-optimized
func (b *Beacon) Value(ctx context.Context, epochID types.EpochID) (uint32, error) {
	// TODO(nkryuchkov): remove when beacon sync is done
	beaconSyncEnabled := false
	if !beaconSyncEnabled {
		return uint32(epochID), nil
	}

	// check cache
	if val, ok := b.cache.Get(epochID); ok {
		return val.(uint32), nil
	}

	// TODO: do we need a lock here?
	v, err := b.beaconGetter.GetBeacon(epochID)
	if err != nil {
		return 0, err
	}

	value := binary.LittleEndian.Uint32(v)
	b.WithContext(ctx).With().Debug("hare eligibility beacon value for epoch",
		epochID,
		log.String("beacon_hex", util.Bytes2Hex(v)),
		log.Uint32("beacon_dec", value))

	// update and return
	b.cache.Add(epochID, value)
	return value, nil
}
