package discovery

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
)

const defaultPeersFileName = "peers.json"
const saveRoutineInterval = time.Minute * 10

type serializedKnownAddress struct {
	Addr        string
	Src         string
	Attempts    int
	LastSeen    int64
	LastAttempt int64
	LastSuccess int64
	// no refcount or tried, that is available from context.
}

type serializedAddrManager struct {
	Key          [32]byte
	Addresses    []*serializedKnownAddress
	NewBuckets   [newBucketCount][]string // Info represented as string
	TriedBuckets [triedBucketCount][]string
}

// savePeers saves all the known addresses to a file so they can be read back
// in at next run.
func (a *addrBook) savePeers(path string) {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	// First we make a serialisable datastructure so we can encode it to
	// json.
	sam := new(serializedAddrManager)
	copy(sam.Key[:], a.key[:])

	sam.Addresses = make([]*serializedKnownAddress, len(a.addrIndex))
	i := 0
	for _, v := range a.addrIndex {
		ska := new(serializedKnownAddress)
		ska.Addr = v.na.String()
		ska.Src = v.srcAddr.String()
		ska.LastSeen = v.lastSeen.Unix()
		ska.Attempts = v.attempts
		ska.LastAttempt = v.lastattempt.Unix()
		ska.LastSuccess = v.lastsuccess.Unix()
		// Tried and refs are implicit in the rest of the structure
		// and will be worked out from context on unserialisation.
		sam.Addresses[i] = ska
		i++
	}
	for i := range a.addrNew {
		sam.NewBuckets[i] = make([]string, len(a.addrNew[i]))
		j := 0
		for _, v := range a.addrNew[i] {
			sam.NewBuckets[i][j] = v.na.String()
			j++
		}
	}
	for i := range a.addrTried {
		sam.TriedBuckets[i] = make([]string, len(a.addrTried[i]))
		j := 0
		for _, v := range a.addrTried[i] {
			sam.TriedBuckets[i][j] = v.na.String()
			j++
		}
	}

	w, err := os.Create(path)
	if err != nil {
		a.logger.Error("Error creating file: %v", err)
		return
	}
	enc := json.NewEncoder(w)
	defer w.Close()
	if err := enc.Encode(&sam); err != nil {
		a.logger.Error("Failed to encode file %s: %v", path, err)
		return
	}
}

// loadPeers loads the known address from the saved file.  If empty, missing, or
// malformed file, just don't load anything and start fresh
func (a *addrBook) loadPeers(filePath string) {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	// we don't lock the mutex in deserializePeers because it might fail and we'll run reset

	err := a.deserializePeers(filePath)
	if err != nil {
		a.logger.With().Error("failed to parse file", log.String("path", filePath), log.Err(err))
		// if it is invalid we nuke the old one unconditionally.
		err = os.Remove(filePath)
		if err != nil {
			a.logger.With().Warning("failed to remove corrupt peers file",
				log.String("path", filePath),
				log.Err(err))
		}
		a.reset()
		return
	}
}

func (a *addrBook) deserializePeers(filePath string) error {
	_, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		a.logger.With().Warning("peers not loaded to addrbook since file does not exist",
			log.String("path", filePath))
		return nil
	}
	r, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("error opening file: %v", err)
	}
	defer r.Close()

	var sam serializedAddrManager
	dec := json.NewDecoder(r)
	err = dec.Decode(&sam)
	if err != nil {
		return fmt.Errorf("error reading %s: %v", filePath, err)
	}

	copy(a.key[:], sam.Key[:])

	for _, v := range sam.Addresses {
		ka := new(KnownAddress)

		ka.na, err = node.ParseNode(v.Addr)
		if err != nil {
			return fmt.Errorf("failed to deserialize netaddress "+
				"%s: %v", v.Addr, err)
		}

		ka.srcAddr, err = node.ParseNode(v.Src)
		if err != nil {
			return fmt.Errorf("failed to deserialize netaddress "+
				"%s: %v", v.Src, err)
		}

		ka.attempts = v.Attempts
		ka.lastattempt = time.Unix(v.LastAttempt, 0)
		ka.lastsuccess = time.Unix(v.LastSuccess, 0)
		a.addrIndex[ka.na.ID] = ka
	}

	for i := range sam.NewBuckets {
		for _, val := range sam.NewBuckets[i] {
			parsed, err := node.ParseNode(val)
			if err != nil {
				a.logger.Warning("a problem occured trying to load peer %v, err=%v", val, err)
				continue
			}
			ka, ok := a.addrIndex[parsed.ID]
			if !ok {
				return fmt.Errorf("newbucket contains %s but "+
					"none in address list", val)
			}

			if ka.refs == 0 {
				a.nNew++
			}
			ka.refs++
			a.addrNew[i][parsed.ID] = ka
		}
	}

	for i := range sam.TriedBuckets {
		for _, val := range sam.TriedBuckets[i] {
			parsed, err := node.ParseNode(val)
			if err != nil {
				a.logger.Warning("a problem occured trying to load peer %v, err=%v", val, err)
				continue
			}
			ka, ok := a.addrIndex[parsed.ID]
			if !ok {
				return fmt.Errorf("tried bucket contains %s but "+
					"none in address list", val)
			}

			if ka.refs == 0 {
				a.nTried++
			}
			ka.refs++
			a.addrTried[i][parsed.ID] = ka
		}
	}

	// Sanity checking.
	for k, v := range a.addrIndex {
		if v.refs == 0 && !v.tried {
			return fmt.Errorf("address %s after serialisation "+
				"with no references", k)
		}

		if v.refs > 0 && v.tried {
			return fmt.Errorf("address %s after serialisation "+
				"which is both new and tried! ", k)
		}
	}

	log.Info("Loaded %d addresses from file '%s'", a.numAddresses(), filePath)

	return nil
}

// addressHandler is the main handler for the address manager.  It must be run
// as a goroutine.
func (a *addrBook) saveRoutine(filepath string) {
	dumpAddressTicker := time.NewTicker(saveRoutineInterval)
	defer dumpAddressTicker.Stop()

out:
	for {
		select {
		case <-dumpAddressTicker.C:
			a.savePeers(filepath)
			a.logger.Debug("Saved peers to file %v", filepath)
		case <-a.quit:
			break out
		}
	}

	a.logger.Debug("Saving peer before exit to file %v", filepath)
	a.savePeers(filepath)
	a.logger.Debug("Address handler done")

}
