package blocks

import (
	"encoding/binary"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// BeaconGetter gets a beacon value.
type BeaconGetter interface {
	GetBeacon(epochNumber types.EpochID) (types.TortoiseBeacon, error)
}

// EpochBeaconProvider holds all the dependencies for generating an epoch beacon. There are currently none.
type EpochBeaconProvider struct{}

// GetBeacon returns a beacon given an epoch ID. The current implementation returns the epoch ID in byte format.
func (p *EpochBeaconProvider) GetBeacon(epochNumber types.EpochID) (types.TortoiseBeacon, error) {
	ret := make([]byte, 32)
	binary.LittleEndian.PutUint32(ret, uint32(epochNumber))
	return ret, nil
}
