package blocks

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

var atxID = types.ATXID([32]byte{1, 3, 3, 7})
var nodeID, vrfsgn = generateNodeIDAndSigner()
var validateVRF = signing.VRFVerify
var edSigner = signing.NewEdSigner()
var activeSetAtxs = []types.ATXID{atxID, atxID, atxID, atxID, atxID, atxID, atxID, atxID, atxID, atxID} // 10 ATXs

const defaultAtxWeight = 1024

func generateNodeIDAndSigner() (types.NodeID, vrfSigner) {
	edPubkey := edSigner.PublicKey()
	vrfSigner, vrfPubkey, err := signing.NewVRFSigner(edPubkey.Bytes())
	if err != nil {
		panic("failed to create vrf signer")
	}
	return types.NodeID{
		Key:          edPubkey.String(),
		VRFPublicKey: vrfPubkey,
	}, vrfSigner
}

type mockActivationDB struct {
	atxPublicationLayer types.LayerID
	atxs                map[string]map[types.LayerID]types.ATXID
	activeSetAtxs       []types.ATXID
	atxErr              error
}

func (a mockActivationDB) GetEpochAtxs(_ types.EpochID) (atxs []types.ATXID) {
	if a.activeSetAtxs == nil {
		return activeSetAtxs
	}
	return a.activeSetAtxs
}

func (a mockActivationDB) GetIdentity(edID string) (types.NodeID, error) {
	return types.NodeID{Key: edID, VRFPublicKey: nodeID.VRFPublicKey}, nil
}

func (a mockActivationDB) GetNodeAtxIDForEpoch(nID types.NodeID, targetEpoch types.EpochID) (types.ATXID, error) {
	if nID.Key != nodeID.Key || targetEpoch == 0 {
		return *types.EmptyATXID, a.atxErr
	}
	return atxID, nil
}

func (a mockActivationDB) GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error) {
	if id == atxID {
		atxHeader := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				NodeID: types.NodeID{
					Key:          edSigner.PublicKey().String(),
					VRFPublicKey: nodeID.VRFPublicKey,
				},
				PubLayerID: a.atxPublicationLayer,
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: defaultAtxWeight,
		}
		atxHeader.SetID(&id)
		return atxHeader, nil
	}
	return nil, errors.New("wrong atx id")
}

func (a mockActivationDB) GetEpochWeight(epochID types.EpochID) (uint64, []types.ATXID, error) {
	return uint64(len(a.GetEpochAtxs(epochID-1))) * defaultAtxWeight, nil, nil
}

func TestBlockOracle(t *testing.T) {
	r := require.New(t)

	// Happy flow with small numbers that can be inspected manually
	testBlockOracleAndValidator(t, r, 10, 20)

	// Big, realistic numbers
	// testBlockOracleAndValidator(r, 3000*defaultAtxWeight, 200, 4032) // commented out because it takes VERY long

	// More miners than blocks (ensure at least one block per activation)
	testBlockOracleAndValidator(t, r, 2, 2)
}

func testBlockOracleAndValidator(t *testing.T, r *require.Assertions, committeeSize uint32, layersPerEpoch uint32) {
	types.SetLayersPerEpoch(layersPerEpoch)
	activationDB := &mockActivationDB{atxPublicationLayer: types.NewLayerID(0)}
	beaconProvider := mockTortoiseBeacon(t)
	lg := logtest.New(t).WithName(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))
	validator := NewBlockEligibilityValidator(committeeSize, layersPerEpoch, activationDB, beaconProvider, validateVRF, nil, lg.WithName("blkElgValidator"))
	numberOfEpochsToTest := uint32(2)
	counterValuesSeen := map[uint32]int{}
	for layer := layersPerEpoch * 2; layer < layersPerEpoch*(numberOfEpochsToTest+2); layer++ {
		activationDB.atxPublicationLayer = types.NewLayerID((layer/layersPerEpoch)*layersPerEpoch - 1)
		layerID := types.NewLayerID(layer)
		tBeacon, err := beaconProvider.GetBeacon(layerID.GetEpoch())
		r.NoError(err)
		_, proofs, _, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)

		for _, proof := range proofs {
			block := newBlockWithEligibility(layerID, atxID, proof, activationDB, tBeacon)
			eligible, err := validator.BlockSignedAndEligible(block)
			r.NoError(err, "at layer %d, with layersPerEpoch %d", layer, layersPerEpoch)
			r.True(eligible, "should be eligible at layer %d, but isn't", layer)
			counterValuesSeen[proof.J]++
		}
	}

	numberOfEligibleBlocks := committeeSize * uint32(layersPerEpoch) / 10
	if numberOfEligibleBlocks == 0 {
		numberOfEligibleBlocks = 1
	}
	for c := uint32(0); c < numberOfEligibleBlocks; c++ {
		r.EqualValues(numberOfEpochsToTest, counterValuesSeen[c],
			"counter value %d expected %d times, but received %d times",
			c, numberOfEpochsToTest, counterValuesSeen[c])
	}
	r.Len(counterValuesSeen, int(numberOfEligibleBlocks))
}

func TestBlockOracleInGenesisReturnsNoAtx(t *testing.T) {
	r := require.New(t)
	committeeSize := uint32(10)
	layersPerEpoch := uint32(20)
	types.SetLayersPerEpoch(layersPerEpoch)

	activationDB := &mockActivationDB{atxPublicationLayer: types.NewLayerID(0)}
	beaconProvider := mockTortoiseBeacon(t)
	lg := logtest.New(t).WithName(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))
	for layer := uint32(0); layer < layersPerEpoch; layer++ {
		activationDB.atxPublicationLayer = types.NewLayerID((layer/layersPerEpoch)*layersPerEpoch - 1)
		layerID := types.NewLayerID(layer)
		atxID, _, _, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)
		r.Equal(*types.EmptyATXID, atxID)
	}
}

func TestBlockOracleEmptyActiveSet(t *testing.T) {
	types.SetLayersPerEpoch(3)
	r := require.New(t)

	committeeSize := uint32(200)
	layersPerEpoch := uint32(10)

	activationDB := &mockActivationDB{atxPublicationLayer: types.NewLayerID(layersPerEpoch), activeSetAtxs: []types.ATXID{}}
	beaconProvider := mockTortoiseBeacon(t)
	lg := logtest.New(t).WithName(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	_, proofs, _, err := blockOracle.BlockEligible(types.NewLayerID(layersPerEpoch * 3))
	r.EqualError(err, "zero total weight not allowed")
	r.Nil(proofs)
}

func TestBlockOracleEmptyActiveSetValidation(t *testing.T) {
	r := require.New(t)

	committeeSize := uint32(200)
	layersPerEpoch := uint32(10)
	types.SetLayersPerEpoch(layersPerEpoch)

	activationDB := &mockActivationDB{atxPublicationLayer: types.NewLayerID(layersPerEpoch)}
	beaconProvider := mockTortoiseBeacon(t)

	lg := logtest.New(t).WithName(nodeID.Key[:5])
	validator := NewBlockEligibilityValidator(committeeSize, layersPerEpoch, activationDB, beaconProvider, validateVRF, nil, lg.WithName("blkElgValidator"))
	layerID := types.NewLayerID(layersPerEpoch * 2)
	tBeacon, err := beaconProvider.GetBeacon(layerID.GetEpoch())
	r.NoError(err)
	block := newBlockWithEligibility(layerID, atxID, types.BlockEligibilityProof{}, activationDB, tBeacon)
	block.ActiveSet = &[]types.ATXID{}
	eligible, err := validator.BlockSignedAndEligible(block)
	r.EqualError(err, "failed to get number of eligible blocks: zero total weight not allowed")
	r.False(eligible)
}

func TestBlockOracleNoActivationsForNode(t *testing.T) {
	r := require.New(t)

	committeeSize := uint32(200)
	layersPerEpoch := uint32(10)
	types.SetLayersPerEpoch(layersPerEpoch)
	seed := make([]byte, 32)
	rand.Read(seed)
	_, publicKey, err := signing.NewVRFSigner(seed)
	r.NoError(err)
	nID := types.NodeID{
		Key:          "other key",
		VRFPublicKey: publicKey,
	} // This guy has no activations 🧐

	activationDB := &mockActivationDB{atxPublicationLayer: types.NewLayerID(layersPerEpoch)}
	beaconProvider := mockTortoiseBeacon(t)
	lg := logtest.New(t).WithName(nID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nID, func() bool { return true }, lg.WithName("blockOracle"))
	activationDB.atxErr = database.ErrNotFound
	_, proofs, _, err := blockOracle.BlockEligible(types.NewLayerID(layersPerEpoch * 2))
	r.Equal(ErrMinerHasNoATXInPreviousEpoch, err)
	r.Nil(proofs)
}

func TestBlockOracleATXLookupError(t *testing.T) {
	r := require.New(t)

	committeeSize := uint32(200)
	layersPerEpoch := uint32(10)
	types.SetLayersPerEpoch(layersPerEpoch)
	seed := make([]byte, 32)
	rand.Read(seed)
	_, publicKey, err := signing.NewVRFSigner(seed)
	r.NoError(err)
	nID := types.NodeID{
		Key:          "other key",
		VRFPublicKey: publicKey,
	} // This guy has no activations 🧐

	activationDB := &mockActivationDB{atxPublicationLayer: types.NewLayerID(layersPerEpoch)}
	beaconProvider := mockTortoiseBeacon(t)
	lg := logtest.New(t).WithName(nID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nID, func() bool { return true }, lg.WithName("blockOracle"))
	activationDB.atxErr = errors.New("not found")
	_, proofs, _, err := blockOracle.BlockEligible(types.NewLayerID(layersPerEpoch * 2))
	r.EqualError(err, "failed to get latest atx for node in epoch 2: failed to get atx id for target epoch 2: not found")
	r.Nil(proofs)
}

func TestBlockOracleValidatorInvalidProof(t *testing.T) {
	r := require.New(t)

	committeeSize := uint32(10)
	layersPerEpoch := uint32(20)
	types.SetLayersPerEpoch(layersPerEpoch)

	activationDB := &mockActivationDB{atxPublicationLayer: types.NewLayerID(layersPerEpoch)}
	beaconProvider := mockTortoiseBeacon(t)
	lg := logtest.New(t).WithName(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	layerID := types.NewLayerID(layersPerEpoch * 2)

	var proof types.BlockEligibilityProof
	for ; ; layerID = layerID.Add(1) {
		_, proofs, _, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)
		if len(proofs) > 0 {
			proof = proofs[0]
			break
		}
	}
	proof.Sig[0]++ // Messing with the proof 😈

	tBeacon, err := beaconProvider.GetBeacon(layerID.GetEpoch())
	r.NoError(err)
	validator := NewBlockEligibilityValidator(committeeSize, layersPerEpoch, activationDB, beaconProvider, validateVRF, nil, lg.WithName("blkElgValidator"))
	block := newBlockWithEligibility(layerID, atxID, proof, activationDB, tBeacon)
	eligible, err := validator.BlockSignedAndEligible(block)
	r.NotNil(err)
	r.Contains(err.Error(), "tortoise beacon eligibility VRF validation failed")
	r.False(eligible)
}

func TestBlockOracleValidatorInvalidProof2(t *testing.T) {
	r := require.New(t)

	committeeSize := uint32(10)
	layersPerEpoch := uint32(1)
	types.SetLayersPerEpoch(layersPerEpoch)
	minerActivationDB := &mockActivationDB{atxPublicationLayer: types.NewLayerID(layersPerEpoch), activeSetAtxs: activeSetAtxs[:1]}
	// minerActivationDB := &mockActivationDB{totalWeight: 1 * defaultAtxWeight, atxPublicationLayer: types.NewLayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.ATXID{}}
	// Use different active set size to get more blocks 🤫
	validatorActivationDB := &mockActivationDB{atxPublicationLayer: types.NewLayerID(layersPerEpoch), activeSetAtxs: activeSetAtxs}
	// validatorActivationDB := &mockActivationDB{totalWeight: 10 * defaultAtxWeight, atxPublicationLayer: types.NewLayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.ATXID{}}

	beaconProvider := mockTortoiseBeacon(t)
	lg := logtest.New(t).WithName(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, minerActivationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	layerID := types.NewLayerID(layersPerEpoch * 2)

	_, proofs, _, err := blockOracle.BlockEligible(layerID)
	r.NoError(err)
	r.NotNil(proofs)

	proof := proofs[0]
	for i := 1; proof.J == 0; i++ {
		proof = proofs[i]
	}

	tBeacon, err := beaconProvider.GetBeacon(layerID.GetEpoch())
	r.NoError(err)
	validator := NewBlockEligibilityValidator(committeeSize, layersPerEpoch, validatorActivationDB, beaconProvider, validateVRF, nil, lg.WithName("blkElgValidator"))
	block := newBlockWithEligibility(layerID, atxID, proof, validatorActivationDB, tBeacon)
	eligible, err := validator.BlockSignedAndEligible(block)
	r.False(eligible)
	r.EqualError(err, fmt.Sprintf("proof counter (%d) must be less than number of eligible blocks (1), totalWeight (10240)", proof.J))
}

func TestBlockOracleValidatorInvalidProof3(t *testing.T) {
	r := require.New(t)

	committeeSize := uint32(10)
	layersPerEpoch := uint32(20)
	types.SetLayersPerEpoch(layersPerEpoch)

	activationDB := &mockActivationDB{atxPublicationLayer: types.NewLayerID(layersPerEpoch)}
	beaconProvider := mockTortoiseBeacon(t)
	lg := logtest.New(t).WithName(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	layerID := types.NewLayerID(layersPerEpoch * 2)

	var proof types.BlockEligibilityProof
	for ; ; layerID = layerID.Add(1) {
		_, proofs, _, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)
		// we want a proof with J != 0, so we must have more than one proof in the list
		if len(proofs) > 1 {
			proof = proofs[0]
			// we keep trying until J != 0
			for i := 1; proof.J == 0; i++ {
				proof = proofs[i]
			}
			break
		}
	}

	tBeacon, err := beaconProvider.GetBeacon(layerID.GetEpoch())
	r.NoError(err)
	validatorActivationDB := &mockActivationDB{atxPublicationLayer: types.NewLayerID(0), atxs: activationDB.atxs}
	validator := NewBlockEligibilityValidator(committeeSize, layersPerEpoch, validatorActivationDB, beaconProvider, validateVRF, nil, lg.WithName("blkElgValidator"))
	block := newBlockWithEligibility(layerID, atxID, proof, activationDB, tBeacon)
	eligible, err := validator.BlockSignedAndEligible(block)
	r.False(eligible)
	r.EqualError(err, "ATX target epoch (1) doesn't match block publication epoch (2)")
}

func TestBlockOracleValidatorRefBlockHasNoActiveSet(t *testing.T) {
	r := require.New(t)

	committeeSize := uint32(10)
	layersPerEpoch := uint32(20)
	types.SetLayersPerEpoch(layersPerEpoch)

	activationDB := &mockActivationDB{atxPublicationLayer: types.NewLayerID(layersPerEpoch)}
	beaconProvider := mockTortoiseBeacon(t)
	lg := logtest.New(t).WithName(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	layerID := types.NewLayerID(layersPerEpoch * 2)
	var proof types.BlockEligibilityProof
	for ; ; layerID = layerID.Add(1) {
		_, proofs, _, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)
		if len(proofs) > 0 {
			proof = proofs[0]
			break
		}
	}

	block := newBlockWithEligibility(layerID, atxID, proof, activationDB, nil)
	refBlock := newBlockWithEligibility(layerID, atxID, proof, activationDB, nil)
	refBlock.ActiveSet = nil
	refBlockID := refBlock.ID()
	block.RefBlock = &refBlockID
	ctrl := gomock.NewController(t)
	mockBlocksDB := mocks.NewMockblockDB(ctrl)
	mockBlocksDB.EXPECT().GetBlock(refBlockID).Return(refBlock, nil).Times(1)

	validator := NewBlockEligibilityValidator(committeeSize, layersPerEpoch, activationDB, beaconProvider, validateVRF, mockBlocksDB, lg.WithName("blkElgValidator"))
	eligible, err := validator.BlockSignedAndEligible(block)
	r.Contains(err.Error(), "failed to get active set from block")
	r.False(eligible)
}

func TestBlockOracleValidatorRefBlockHasNoBeacon(t *testing.T) {
	r := require.New(t)

	committeeSize := uint32(10)
	layersPerEpoch := uint32(20)
	types.SetLayersPerEpoch(layersPerEpoch)

	activationDB := &mockActivationDB{atxPublicationLayer: types.NewLayerID(layersPerEpoch)}
	beaconProvider := mockTortoiseBeacon(t)
	lg := logtest.New(t).WithName(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	layerID := types.NewLayerID(layersPerEpoch * 2)
	var proof types.BlockEligibilityProof
	for ; ; layerID = layerID.Add(1) {
		_, proofs, _, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)
		if len(proofs) > 0 {
			proof = proofs[0]
			break
		}
	}

	block := newBlockWithEligibility(layerID, atxID, proof, activationDB, nil)
	refBlock := newBlockWithEligibility(layerID, atxID, proof, activationDB, nil)
	refBlockID := refBlock.ID()
	block.RefBlock = &refBlockID
	ctrl := gomock.NewController(t)
	mockBlocksDB := mocks.NewMockblockDB(ctrl)
	mockBlocksDB.EXPECT().GetBlock(refBlockID).Return(refBlock, nil).Times(1)

	validator := NewBlockEligibilityValidator(committeeSize, layersPerEpoch, activationDB, beaconProvider, validateVRF, mockBlocksDB, lg.WithName("blkElgValidator"))
	eligible, err := validator.BlockSignedAndEligible(block)
	r.Contains(err.Error(), "failed to get tortoise beacon from block")
	r.False(eligible)
}

func TestBlockOracleValidatorWrongBeacon(t *testing.T) {
	r := require.New(t)

	committeeSize := uint32(10)
	layersPerEpoch := uint32(20)
	types.SetLayersPerEpoch(layersPerEpoch)

	activationDB := &mockActivationDB{atxPublicationLayer: types.NewLayerID(layersPerEpoch)}
	beaconProvider := mockTortoiseBeacon(t)
	lg := logtest.New(t).WithName(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	layerID := types.NewLayerID(layersPerEpoch * 2)
	var proof types.BlockEligibilityProof
	for ; ; layerID = layerID.Add(1) {
		_, proofs, _, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)
		if len(proofs) > 0 {
			proof = proofs[0]
			break
		}
	}
	validator := NewBlockEligibilityValidator(committeeSize, layersPerEpoch, activationDB, beaconProvider, validateVRF, nil, lg.WithName("blkElgValidator"))
	block := newBlockWithEligibility(layerID, atxID, proof, activationDB, types.HexToHash32("0x12345678").Bytes())

	eligible, err := validator.BlockSignedAndEligible(block)
	r.Contains(err.Error(), "tortoise beacon eligibility VRF validation failed")
	r.False(eligible)
}

func newBlockWithEligibility(layerID types.LayerID, atxID types.ATXID, proof types.BlockEligibilityProof,
	db *mockActivationDB, tBeacon []byte) *types.Block {

	block := &types.Block{MiniBlock: types.MiniBlock{BlockHeader: types.BlockHeader{
		LayerIndex:       layerID,
		ATXID:            atxID,
		EligibilityProof: proof,
	}}}
	epochAtxs := db.GetEpochAtxs(layerID.GetEpoch())
	block.ActiveSet = &epochAtxs
	block.TortoiseBeacon = tBeacon
	block.Signature = edSigner.Sign(block.Bytes())
	if db.atxs == nil {
		db.atxs = map[string]map[types.LayerID]types.ATXID{}
	}
	if _, ok := db.atxs[edSigner.PublicKey().String()]; !ok {
		db.atxs[edSigner.PublicKey().String()] = map[types.LayerID]types.ATXID{}
	}
	block.Initialize()
	db.atxs[edSigner.PublicKey().String()][layerID] = atxID
	return block
}

func TestBlockEligibility_calc(t *testing.T) {
	r := require.New(t)
	atxH := types.NewActivationTx(types.NIPostChallenge{}, types.Address{}, nil, 0, nil)
	atxDb := &mockAtxDB{atxH: atxH.ActivationTxHeader}
	o := NewMinerBlockOracle(10, 1, atxDb, mockTortoiseBeacon(t), vrfsgn, nodeID, func() bool { return true }, logtest.New(t).WithName(t.Name()))
	o.atx = atxH.ActivationTxHeader
	_, err := o.calcEligibilityProofs(1)
	r.EqualError(err, "zero total weight not allowed") // a hack to make sure we got genesis active set size on genesis
}

func TestMinerBlockOracle_GetEligibleLayers(t *testing.T) {
	r := require.New(t)
	committeeSize := uint32(10)
	layersPerEpoch := uint32(20)
	types.SetLayersPerEpoch(layersPerEpoch)

	activationDB := &mockActivationDB{atxPublicationLayer: types.NewLayerID(0)}
	beaconProvider := mockTortoiseBeacon(t)
	lg := logtest.New(t).WithName(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))
	numberOfEpochsToTest := 1 // this test supports only 1 epoch
	eligibleLayers := 0
	for layer := layersPerEpoch * 2; layer < layersPerEpoch*uint32(numberOfEpochsToTest+2); layer++ {
		activationDB.atxPublicationLayer = types.NewLayerID((layer/layersPerEpoch)*layersPerEpoch - 1)
		layerID := types.NewLayerID(layer)
		_, proofs, _, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)
		if len(proofs) > 0 {
			eligibleLayers++
		}
	}
	r.Equal(eligibleLayers, len(blockOracle.GetEligibleLayers()))

}
