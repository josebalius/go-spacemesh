package mesh

import (
	"bytes"
	"context"
	"math"
	"math/big"
	"os"
	"path"
	"sort"
	"testing"
	"time"

	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	Path = "../tmp/mdb"
)

func teardown() {
	_ = os.RemoveAll(Path)
}

func getMeshDB(tb testing.TB) *DB {
	tb.Helper()
	return NewMemMeshDB(logtest.New(tb))
}

func TestMeshDB_New(t *testing.T) {
	mdb := getMeshDB(t)
	bl := types.NewExistingBlock(types.NewLayerID(1), []byte(rand.String(8)), nil)
	err := mdb.AddBlock(bl)
	assert.NoError(t, err)
	block, err := mdb.GetBlock(bl.ID())
	assert.NoError(t, err)
	assert.True(t, bl.ID() == block.ID())
}

func TestMeshDB_AddBlock(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	defer mdb.Close()
	coinbase := types.HexToAddress("aaaa")

	block1 := types.NewExistingBlock(types.NewLayerID(1), []byte("data1"), nil)

	addTransactionsWithFee(t, mdb, block1, 4, rand.Int63n(100))

	poetRef := []byte{0xba, 0x05}
	atx := newActivationTx(types.NodeID{Key: "aaaa", VRFPublicKey: []byte("bbb")}, 1, types.ATXID{}, types.NewLayerID(5), 1, types.ATXID{}, coinbase, 5, []types.BlockID{}, &types.NIPost{
		Challenge: &types.Hash32{},
		Post: &types.Post{
			Nonce:   0,
			Indices: []byte(nil),
		},
		PostMetadata: &types.PostMetadata{
			Challenge: poetRef,
		},
	})
	var atxs []types.ATXID
	atxs = append(atxs, atx.ID())
	block1.ActiveSet = &atxs
	err := mdb.AddBlock(block1)
	assert.NoError(t, err)

	rBlock1, err := mdb.GetBlock(block1.ID())
	assert.NoError(t, err)

	assert.Equal(t, block1.ID(), rBlock1.ID())
	assert.Equal(t, block1.MinerID(), rBlock1.MinerID())
	assert.Equal(t, len(rBlock1.TxIDs), len(block1.TxIDs), "block content was wrong")
	assert.Equal(t, len(*rBlock1.ActiveSet), len(*block1.ActiveSet), "block content was wrong")
}

func chooseRandomPattern(blocksInLayer int, patternSize int) []int {
	p := rand.Perm(blocksInLayer)
	indexes := make([]int, 0, patternSize)
	for _, r := range p[:patternSize] {
		indexes = append(indexes, r)
	}
	return indexes
}

func createLayerWithRandVoting(index types.LayerID, prev []*types.Layer, blocksInLayer int, patternSize int, lg log.Log) *types.Layer {
	l := types.NewLayer(index)
	var patterns [][]int
	for _, l := range prev {
		blocks := l.Blocks()
		blocksInPrevLayer := len(blocks)
		patterns = append(patterns, chooseRandomPattern(blocksInPrevLayer, int(math.Min(float64(blocksInPrevLayer), float64(patternSize)))))
	}
	layerBlocks := make([]types.BlockID, 0, blocksInLayer)
	for i := 0; i < blocksInLayer; i++ {
		bl := types.NewExistingBlock(types.NewLayerID(0), []byte(rand.String(8)), nil)
		voted := make(map[types.BlockID]struct{})
		layerBlocks = append(layerBlocks, bl.ID())
		for idx, pat := range patterns {
			for _, id := range pat {
				b := prev[idx].Blocks()[id]
				bl.ForDiff = append(bl.ForDiff, b.ID())
				voted[b.ID()] = struct{}{}
			}
		}
		for _, prevBloc := range prev[0].Blocks() {
			if _, ok := voted[prevBloc.ID()]; !ok {
				bl.AgainstDiff = append(bl.AgainstDiff, prevBloc.ID())
			}
		}
		bl.LayerIndex = index
		l.AddBlock(bl)
	}
	lg.Info("Created mesh.LayerID %d with blocks %d", l.Index(), layerBlocks)
	return l
}

func TestForEachInView_Persistent(t *testing.T) {
	mdb, err := NewPersistentMeshDB(Path+"/mesh_db/", 5, logtest.New(t))
	require.NoError(t, err)
	defer mdb.Close()
	defer teardown()
	testForeachInView(mdb, t)
}

func TestForEachInView_InMem(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	testForeachInView(mdb, t)
}

func testForeachInView(mdb *DB, t *testing.T) {
	blocks := make(map[types.BlockID]*types.Block)
	l := GenesisLayer()
	/*gen := l.Blocks()[0]
	blocks[gen.ID()] = gen

	if err := mdb.AddBlock(gen); err != nil {
		t.Fail()
	}*/

	for i := 0; i < 4; i++ {
		lyr := createLayerWithRandVoting(l.Index().Add(1), []*types.Layer{l}, 2, 2, logtest.New(t))
		for _, b := range lyr.Blocks() {
			blocks[b.ID()] = b
			err := mdb.AddBlock(b)
			assert.NoError(t, err)
		}
		l = lyr
	}
	mp := map[types.BlockID]struct{}{}
	foo := func(nb *types.Block) (bool, error) {
		mp[nb.ID()] = struct{}{}
		return false, nil
	}
	ids := map[types.BlockID]struct{}{}
	for _, b := range l.Blocks() {
		ids[b.ID()] = struct{}{}
	}
	err := mdb.ForBlockInView(ids, types.NewLayerID(0), foo)
	assert.NoError(t, err)
	for _, bl := range blocks {
		_, found := mp[bl.ID()]
		assert.True(t, found, "did not process block  ", bl)
	}
}

func TestForEachInView_InMem_WithStop(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	blocks := make(map[types.BlockID]*types.Block)
	l := GenesisLayer()
	gen := l.Blocks()[0]
	blocks[gen.ID()] = gen

	for i := 0; i < 4; i++ {
		lyr := createLayerWithRandVoting(l.Index().Add(1), []*types.Layer{l}, 2, 2, logtest.New(t))
		for _, b := range lyr.Blocks() {
			blocks[b.ID()] = b
			err := mdb.AddBlock(b)
			assert.NoError(t, err)
		}
		l = lyr
	}
	mp := map[types.BlockID]struct{}{}
	i := 0
	foo := func(nb *types.Block) (bool, error) {
		mp[nb.ID()] = struct{}{}
		i++
		return i == 5, nil
	}
	ids := map[types.BlockID]struct{}{}
	for _, b := range l.Blocks() {
		ids[b.ID()] = struct{}{}
	}
	err := mdb.ForBlockInView(ids, types.NewLayerID(0), foo)
	assert.NoError(t, err)
	assert.Equal(t, 5, i)
}

func TestForEachInView_InMem_WithLimitedLayer(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	blocks := make(map[types.BlockID]*types.Block)
	l := GenesisLayer()

	for i := 0; i < 4; i++ {
		lyr := createLayerWithRandVoting(l.Index().Add(1), []*types.Layer{l}, 2, 2, logtest.New(t))
		for _, b := range lyr.Blocks() {
			blocks[b.ID()] = b
			err := mdb.AddBlock(b)
			assert.NoError(t, err)
		}
		l = lyr
	}
	mp := map[types.BlockID]struct{}{}
	i := 0
	foo := func(nb *types.Block) (bool, error) {
		mp[nb.ID()] = struct{}{}
		i++
		return false, nil
	}
	ids := map[types.BlockID]struct{}{}
	for _, b := range l.Blocks() {
		ids[b.ID()] = struct{}{}
	}
	// traverse until (and including) layer 2
	err := mdb.ForBlockInView(ids, types.NewLayerID(2), foo)
	assert.NoError(t, err)
	assert.Equal(t, 9, i)
}

func BenchmarkNewPersistentMeshDB(b *testing.B) {
	const batchSize = 50

	r := require.New(b)

	mdb, err := NewPersistentMeshDB(path.Join(Path, "mesh_db"), 5, logtest.New(b))
	require.NoError(b, err)
	defer mdb.Close()
	defer teardown()

	l := GenesisLayer()
	gen := l.Blocks()[0]

	err = mdb.AddBlock(gen)
	r.NoError(err)

	start := time.Now()
	lStart := time.Now()
	for i := 0; i < 10*batchSize; i++ {
		lyr := createLayerWithRandVoting(l.Index().Add(1), []*types.Layer{l}, 200, 20, logtest.New(b))
		for _, b := range lyr.Blocks() {
			err := mdb.AddBlock(b)
			r.NoError(err)
		}
		l = lyr
		if i%batchSize == batchSize-1 {
			b.Logf("layers %3d-%3d took %12v\t", i-(batchSize-1), i, time.Since(lStart))
			lStart = time.Now()
			for i := 0; i < 100; i++ {
				for _, b := range lyr.Blocks() {
					block, err := mdb.GetBlock(b.ID())
					r.NoError(err)
					r.NotNil(block)
				}
			}
			b.Logf("reading last layer 100 times took %v\n", time.Since(lStart))
			lStart = time.Now()
		}
	}
	b.Logf("\n>>> Total time: %v\n\n", time.Since(start))
}

const (
	initialNonce   = 0
	initialBalance = 100
)

func address() types.Address {
	var addr [20]byte
	copy(addr[:], "12345678901234567890")
	return addr
}

func newTx(r *require.Assertions, signer *signing.EdSigner, nonce, totalAmount uint64) *types.Transaction {
	feeAmount := uint64(1)
	tx, err := types.NewSignedTx(nonce, types.Address{}, totalAmount-feeAmount, 3, feeAmount, signer)
	r.NoError(err)
	return tx
}

func newTxWithDest(r *require.Assertions, signer *signing.EdSigner, dest types.Address, nonce, totalAmount uint64) *types.Transaction {
	feeAmount := uint64(1)
	tx, err := types.NewSignedTx(nonce, dest, totalAmount-feeAmount, 3, feeAmount, signer)
	r.NoError(err)
	return tx
}

func newSignerAndAddress(r *require.Assertions, seedStr string) (*signing.EdSigner, types.Address) {
	seed := make([]byte, 32)
	copy(seed, seedStr)
	_, privKey, err := ed25519.GenerateKey(bytes.NewReader(seed))
	r.NoError(err)
	signer, err := signing.NewEdSignerFromBuffer(privKey)
	r.NoError(err)
	var addr types.Address
	addr.SetBytes(signer.PublicKey().Bytes())
	return signer, addr
}

func TestMeshDB_GetStateProjection(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(logtest.New(t))
	signer, origin := newSignerAndAddress(r, "123")
	err := mdb.addToUnappliedTxs([]*types.Transaction{
		newTx(r, signer, 0, 10),
		newTx(r, signer, 1, 20),
	}, types.NewLayerID(1))
	r.NoError(err)

	nonce, balance, err := mdb.GetProjection(origin, initialNonce, initialBalance)
	r.NoError(err)
	r.Equal(initialNonce+2, int(nonce))
	r.Equal(initialBalance-30, int(balance))
}

func TestMeshDB_GetStateProjection_WrongNonce(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(logtest.New(t))
	signer, origin := newSignerAndAddress(r, "123")
	err := mdb.addToUnappliedTxs([]*types.Transaction{
		newTx(r, signer, 1, 10),
		newTx(r, signer, 2, 20),
	}, types.NewLayerID(1))
	r.NoError(err)

	nonce, balance, err := mdb.GetProjection(origin, initialNonce, initialBalance)
	r.NoError(err)
	r.Equal(initialNonce, int(nonce))
	r.Equal(initialBalance, int(balance))
}

func TestMeshDB_GetStateProjection_DetectNegativeBalance(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(logtest.New(t))
	signer, origin := newSignerAndAddress(r, "123")
	err := mdb.addToUnappliedTxs([]*types.Transaction{
		newTx(r, signer, 0, 10),
		newTx(r, signer, 1, 95),
	}, types.NewLayerID(1))
	r.NoError(err)

	nonce, balance, err := mdb.GetProjection(origin, initialNonce, initialBalance)
	r.NoError(err)
	r.Equal(1, int(nonce))
	r.Equal(initialBalance-10, int(balance))
}

func TestMeshDB_GetStateProjection_NothingToApply(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(logtest.New(t))

	nonce, balance, err := mdb.GetProjection(address(), initialNonce, initialBalance)
	r.NoError(err)
	r.Equal(uint64(initialNonce), nonce)
	r.Equal(uint64(initialBalance), balance)
}

func TestMeshDB_UnappliedTxs(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(logtest.New(t))

	signer1, origin1 := newSignerAndAddress(r, "thc")
	signer2, origin2 := newSignerAndAddress(r, "cbd")
	err := mdb.addToUnappliedTxs([]*types.Transaction{
		newTx(r, signer1, 420, 240),
		newTx(r, signer1, 421, 241),
		newTx(r, signer2, 0, 100),
		newTx(r, signer2, 1, 101),
	}, types.NewLayerID(1))
	r.NoError(err)

	txns1 := getTxns(r, mdb, origin1)
	r.Len(txns1, 2)
	r.Equal(420, int(txns1[0].Nonce))
	r.Equal(421, int(txns1[1].Nonce))
	r.Equal(240, int(txns1[0].TotalAmount))
	r.Equal(241, int(txns1[1].TotalAmount))

	txns2 := getTxns(r, mdb, origin2)
	r.Len(txns2, 2)
	r.Equal(0, int(txns2[0].Nonce))
	r.Equal(1, int(txns2[1].Nonce))
	r.Equal(100, int(txns2[0].TotalAmount))
	r.Equal(101, int(txns2[1].TotalAmount))

	mdb.removeFromUnappliedTxs([]*types.Transaction{
		newTx(r, signer2, 0, 100),
	})

	txns1 = getTxns(r, mdb, origin1)
	r.Len(txns1, 2)
	r.Equal(420, int(txns1[0].Nonce))
	r.Equal(421, int(txns1[1].Nonce))
	r.Equal(240, int(txns1[0].TotalAmount))
	r.Equal(241, int(txns1[1].TotalAmount))

	txns2 = getTxns(r, mdb, origin2)
	r.Len(txns2, 1)
	r.Equal(1, int(txns2[0].Nonce))
	r.Equal(101, int(txns2[0].TotalAmount))
}

func TestMeshDB_testGetTransactions(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(logtest.New(t))

	signer1, addr1 := newSignerAndAddress(r, "thc")
	signer2, _ := newSignerAndAddress(r, "cbd")
	_, addr3 := newSignerAndAddress(r, "cbe")
	blk := &types.Block{}
	blk.LayerIndex = types.NewLayerID(1)
	err := mdb.writeTransactions(blk,
		newTx(r, signer1, 420, 240),
		newTx(r, signer1, 421, 241),
		newTxWithDest(r, signer2, addr1, 0, 100),
		newTxWithDest(r, signer2, addr1, 1, 101),
	)
	r.NoError(err)

	txs, err := mdb.GetTransactionsByOrigin(types.NewLayerID(1), addr1)
	r.NoError(err)
	r.Equal(2, len(txs))

	txs, err = mdb.GetTransactionsByDestination(types.NewLayerID(1), addr1)
	r.NoError(err)
	r.Equal(2, len(txs))

	// test negative case
	txs, err = mdb.GetTransactionsByOrigin(types.NewLayerID(1), addr3)
	r.NoError(err)
	r.Equal(0, len(txs))

	txs, err = mdb.GetTransactionsByDestination(types.NewLayerID(1), addr3)
	r.NoError(err)
	r.Equal(0, len(txs))
}

type TinyTx struct {
	ID          types.TransactionID
	Nonce       uint64
	TotalAmount uint64
}

func getTxns(r *require.Assertions, mdb *DB, origin types.Address) []TinyTx {
	txns, err := mdb.getAccountPendingTxs(origin)
	r.NoError(err)
	var ret []TinyTx
	for nonce, nonceTxs := range txns.PendingTxs {
		for id, tx := range nonceTxs {
			ret = append(ret, TinyTx{ID: id, Nonce: nonce, TotalAmount: tx.Amount + tx.Fee})
		}
	}
	sort.Slice(ret, func(i, j int) bool {
		return ret[i].Nonce < ret[j].Nonce
	})
	return ret
}

func TestMeshDB_testGetRewards(t *testing.T) {
	r := require.New(t)
	mdb := NewMemMeshDB(logtest.New(t))
	signer1, addr1 := newSignerAndAddress(r, "123")
	signer2, addr2 := newSignerAndAddress(r, "456")
	signer3, addr3 := newSignerAndAddress(r, "789")
	_, addr4 := newSignerAndAddress(r, "999")

	smesher1 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer1.PublicKey().Bytes(),
	}
	smesher2 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer2.PublicKey().Bytes(),
	}
	smesher3 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer3.PublicKey().Bytes(),
	}

	smesher1String := smesher1.String()
	smesher2String := smesher2.String()
	smesher3String := smesher3.String()

	test1Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher1String: 1,
		},
		addr2: {
			smesher2String: 1,
		},
		addr3: {
			smesher3String: 1,
		},
	}

	test2Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher1String: 1,
		},
		addr2: {
			smesher2String: 1,
		},
	}

	test3Map := map[types.Address]map[string]uint64{
		addr2: {
			smesher2String: 2,
		},
	}

	err := mdb.writeTransactionRewards(types.NewLayerID(1), test1Map, big.NewInt(10000), big.NewInt(9000))
	r.NoError(err)

	err = mdb.writeTransactionRewards(types.NewLayerID(2), test2Map, big.NewInt(20000), big.NewInt(19000))
	r.NoError(err)

	err = mdb.writeTransactionRewards(types.NewLayerID(3), test3Map, big.NewInt(15000), big.NewInt(14500))
	r.NoError(err)

	rewards, err := mdb.GetRewards(addr2)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher2, Coinbase: addr2},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher2, Coinbase: addr2},
		{Layer: types.NewLayerID(3), TotalReward: 30000, LayerRewardEstimate: 29000, SmesherID: smesher2, Coinbase: addr2},
	}, rewards)

	rewards, err = mdb.GetRewards(addr1)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher1, Coinbase: addr1},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher1, Coinbase: addr1},
	}, rewards)

	rewards, err = mdb.GetRewards(addr4)
	r.NoError(err)
	r.Nil(rewards)
}

func TestMeshDB_testGetRewardsBySmesher(t *testing.T) {
	r := require.New(t)
	mdb := NewMemMeshDB(logtest.New(t))
	signer1, addr1 := newSignerAndAddress(r, "123")
	signer2, addr2 := newSignerAndAddress(r, "456")
	signer3, addr3 := newSignerAndAddress(r, "789")
	signer4, _ := newSignerAndAddress(r, "999")

	smesher1 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer1.PublicKey().Bytes(),
	}
	smesher2 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer2.PublicKey().Bytes(),
	}
	smesher3 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer3.PublicKey().Bytes(),
	}
	smesher4 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer4.PublicKey().Bytes(),
	}

	smesher1String := smesher1.String()
	smesher2String := smesher2.String()
	smesher3String := smesher3.String()

	test1Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher1String: 1,
		},
		addr2: {
			smesher2String: 1,
		},
		addr3: {
			smesher3String: 1,
		},
	}

	test2Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher1String: 1,
		},
		addr2: {
			smesher2String: 1,
		},
	}

	test3Map := map[types.Address]map[string]uint64{
		addr2: {
			smesher2String: 2,
		},
	}

	err := mdb.writeTransactionRewards(types.NewLayerID(1), test1Map, big.NewInt(10000), big.NewInt(9000))
	r.NoError(err)

	err = mdb.writeTransactionRewards(types.NewLayerID(2), test2Map, big.NewInt(20000), big.NewInt(19000))
	r.NoError(err)

	err = mdb.writeTransactionRewards(types.NewLayerID(3), test3Map, big.NewInt(15000), big.NewInt(14500))
	r.NoError(err)

	rewards, err := mdb.GetRewardsBySmesherID(smesher2)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher2, Coinbase: addr2},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher2, Coinbase: addr2},
		{Layer: types.NewLayerID(3), TotalReward: 30000, LayerRewardEstimate: 29000, SmesherID: smesher2, Coinbase: addr2},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher1)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher1, Coinbase: addr1},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher1, Coinbase: addr1},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher4)
	r.NoError(err)
	r.Nil(rewards)
}

func TestMeshDB_testGetRewardsBySmesherChangingLayer(t *testing.T) {
	r := require.New(t)
	mdb := NewMemMeshDB(logtest.New(t))
	signer1, addr1 := newSignerAndAddress(r, "123")
	signer2, addr2 := newSignerAndAddress(r, "456")
	signer3, addr3 := newSignerAndAddress(r, "789")
	signer4, _ := newSignerAndAddress(r, "999")

	smesher1 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer1.PublicKey().Bytes(),
	}
	smesher2 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer2.PublicKey().Bytes(),
	}
	smesher3 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer3.PublicKey().Bytes(),
	}
	smesher4 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer4.PublicKey().Bytes(),
	}

	smesher1String := smesher1.String()
	smesher2String := smesher2.String()
	smesher3String := smesher3.String()

	test1Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher1String: 1,
		},
		addr2: {
			smesher2String: 1,
		},
		addr3: {
			smesher3String: 1,
		},
	}

	test2Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher2String: 1,
		},
		addr2: {
			smesher3String: 1,
		},
	}

	test3Map := map[types.Address]map[string]uint64{
		addr2: {
			smesher2String: 2,
		},
	}

	err := mdb.writeTransactionRewards(types.NewLayerID(1), test1Map, big.NewInt(10000), big.NewInt(9000))
	r.NoError(err)

	err = mdb.writeTransactionRewards(types.NewLayerID(2), test2Map, big.NewInt(20000), big.NewInt(19000))
	r.NoError(err)

	err = mdb.writeTransactionRewards(types.NewLayerID(3), test3Map, big.NewInt(15000), big.NewInt(14500))
	r.NoError(err)

	rewards, err := mdb.GetRewardsBySmesherID(smesher2)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher2, Coinbase: addr2},
		{Layer: types.NewLayerID(3), TotalReward: 30000, LayerRewardEstimate: 29000, SmesherID: smesher2, Coinbase: addr2},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher2, Coinbase: addr1},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher1)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher1, Coinbase: addr1},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher3)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher3, Coinbase: addr2},
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher3, Coinbase: addr3},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher4)
	r.NoError(err)
	r.Nil(rewards)
}

func TestMeshDB_testGetRewardsBySmesherMultipleSmeshers(t *testing.T) {
	r := require.New(t)
	mdb := NewMemMeshDB(logtest.New(t))
	signer1, addr1 := newSignerAndAddress(r, "123")
	signer2, addr2 := newSignerAndAddress(r, "456")
	signer3, addr3 := newSignerAndAddress(r, "789")
	signer4, _ := newSignerAndAddress(r, "999")

	smesher1 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer1.PublicKey().Bytes(),
	}
	smesher2 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer2.PublicKey().Bytes(),
	}
	smesher3 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer3.PublicKey().Bytes(),
	}
	smesher4 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer4.PublicKey().Bytes(),
	}

	smesher1String := smesher1.String()
	smesher2String := smesher2.String()
	smesher3String := smesher3.String()
	smesher4String := smesher4.String()

	test1Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher1String: 1,
			smesher4String: 1,
		},
		addr2: {
			smesher2String: 1,
		},
		addr3: {
			smesher3String: 1,
		},
	}

	err := mdb.writeTransactionRewards(types.NewLayerID(1), test1Map, big.NewInt(10000), big.NewInt(9000))
	r.NoError(err)

	rewards, err := mdb.GetRewardsBySmesherID(smesher2)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher2, Coinbase: addr2},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher1)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher1, Coinbase: addr1},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher3)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher3, Coinbase: addr3},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher4)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher4, Coinbase: addr1},
	}, rewards)

	rewards, err = mdb.GetRewards(addr1)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher1, Coinbase: addr1},
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher4, Coinbase: addr1},
	}, rewards)
}

func TestMeshDB_testGetRewardsBySmesherMultipleSmeshersAndLayers(t *testing.T) {
	r := require.New(t)
	mdb := NewMemMeshDB(logtest.New(t))
	signer1, addr1 := newSignerAndAddress(r, "123")
	signer2, addr2 := newSignerAndAddress(r, "456")
	signer3, addr3 := newSignerAndAddress(r, "789")
	signer4, _ := newSignerAndAddress(r, "999")

	smesher1 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer1.PublicKey().Bytes(),
	}
	smesher2 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer2.PublicKey().Bytes(),
	}
	smesher3 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer3.PublicKey().Bytes(),
	}
	smesher4 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer4.PublicKey().Bytes(),
	}

	smesher1String := smesher1.String()
	smesher2String := smesher2.String()
	smesher3String := smesher3.String()
	smesher4String := smesher4.String()

	test1Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher1String: 1,
			smesher4String: 1,
		},
		addr2: {
			smesher2String: 1,
		},
		addr3: {
			smesher3String: 1,
		},
	}

	test2Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher1String: 1,
			smesher4String: 1,
		},
		addr2: {
			smesher2String: 1,
		},
		addr3: {
			smesher3String: 1,
		},
	}

	err := mdb.writeTransactionRewards(types.NewLayerID(1), test1Map, big.NewInt(10000), big.NewInt(9000))
	r.NoError(err)

	err = mdb.writeTransactionRewards(types.NewLayerID(2), test2Map, big.NewInt(20000), big.NewInt(19000))
	r.NoError(err)

	rewards, err := mdb.GetRewardsBySmesherID(smesher2)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher2, Coinbase: addr2},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher2, Coinbase: addr2},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher1)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher1, Coinbase: addr1},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher1, Coinbase: addr1},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher3)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher3, Coinbase: addr3},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher3, Coinbase: addr3},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher4)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher4, Coinbase: addr1},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher4, Coinbase: addr1},
	}, rewards)

	rewards, err = mdb.GetRewards(addr1)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher1, Coinbase: addr1},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher1, Coinbase: addr1},
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher4, Coinbase: addr1},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher4, Coinbase: addr1},
	}, rewards)
}

func TestMeshDB_RecordCoinFlip(t *testing.T) {
	r := require.New(t)
	layerID := types.NewLayerID(123)

	testCoinflip := func(mdb *DB) {
		_, exists := mdb.GetCoinflip(context.TODO(), layerID)
		r.False(exists, "coin value should not exist before being inserted")
		mdb.RecordCoinflip(context.TODO(), layerID, true)
		coin, exists := mdb.GetCoinflip(context.TODO(), layerID)
		r.True(exists, "expected coin value to exist")
		r.True(coin, "expected true coin value")
		mdb.RecordCoinflip(context.TODO(), layerID, false)
		coin, exists = mdb.GetCoinflip(context.TODO(), layerID)
		r.True(exists, "expected coin value to exist")
		r.False(coin, "expected false coin value on overwrite")
	}

	mdb1 := NewMemMeshDB(logtest.New(t))
	defer mdb1.Close()
	testCoinflip(mdb1)
	mdb2, err := NewPersistentMeshDB(Path+"/mesh_db/", 5, logtest.New(t))
	require.NoError(t, err)
	defer mdb2.Close()
	defer teardown()
	testCoinflip(mdb2)
}

func TestMeshDB_GetMeshTransactions(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(logtest.New(t))

	signer1, _ := newSignerAndAddress(r, "thc")

	blk := &types.Block{}
	blk.LayerIndex = types.NewLayerID(1)
	var (
		nonce  uint64
		ids    []types.TransactionID
		layers = 10
	)
	for i := 1; i <= layers; i++ {
		nonce++
		blk.LayerIndex = types.NewLayerID(uint32(i))
		tx := newTx(r, signer1, nonce, 240)
		ids = append(ids, tx.ID())
		r.NoError(mdb.writeTransactions(blk, tx))
	}
	txs, missing := mdb.GetMeshTransactions(ids)
	r.Len(missing, 0)
	for i := 1; i < layers; i++ {
		r.Equal(ids[i-1], txs[i-1].ID())
		r.EqualValues(types.NewLayerID(uint32(i)), txs[i-1].LayerID)
	}
}

func TestMesh_FindOnce(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	defer mdb.Close()

	r := require.New(t)
	signer1, addr1 := newSignerAndAddress(r, "thc")
	signer2, _ := newSignerAndAddress(r, "cbd")

	blk := &types.Block{}
	layers := []uint32{1, 10, 100}
	nonce := uint64(0)
	for _, layer := range layers {
		blk.LayerIndex = types.NewLayerID(layer)
		nonce++
		err := mdb.writeTransactions(blk,
			newTx(r, signer1, nonce, 100),
			newTxWithDest(r, signer2, addr1, nonce, 100),
		)
		r.NoError(err)
	}
	t.Run("ByDestination", func(t *testing.T) {
		for _, layer := range layers {
			txs, err := mdb.GetTransactionsByDestination(types.NewLayerID(layer), addr1)
			require.NoError(t, err)
			assert.Len(t, txs, 1)
		}
	})

	t.Run("ByOrigin", func(t *testing.T) {
		for _, layer := range layers {
			txs, err := mdb.GetTransactionsByOrigin(types.NewLayerID(layer), addr1)
			require.NoError(t, err)
			assert.Len(t, txs, 1)
		}
	})
}

func BenchmarkGetBlockHeader(b *testing.B) {
	// blocks is set to be twice as large as cache to avoid hitting the cache
	cache := layerSize
	blocks := make([]*types.Block, cache*2)
	db, err := NewPersistentMeshDB(b.TempDir(),
		1, /*size of the cache is multiplied by a constant (layerSize). for the benchmark it needs to be no more than layerSize*/
		logtest.New(b))
	require.NoError(b, err)
	for i := range blocks {
		blocks[i] = types.NewExistingBlock(types.NewLayerID(1), []byte(rand.String(8)), nil)
		require.NoError(b, db.AddBlock(blocks[i]))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		block := blocks[i%len(blocks)]
		_, err = db.GetBlock(block.ID())
		require.NoError(b, err)
	}
}
