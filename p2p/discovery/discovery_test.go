package discovery

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tstBootstrapTimeout = 5 * time.Minute

func TestNew(t *testing.T) {
	ln, nodeinfo := node.GenerateTestNode(t)

	cfg := config.DefaultConfig()
	sim := service.NewSimulator()

	n1 := sim.NewNodeFrom(nodeinfo)

	d := New(context.TODO(), ln, cfg.SwarmConfig, n1, "", logtest.New(t).WithName(t.Name()))
	assert.NotNil(t, d, "D is not nil")
}

func simNodeWithDHT(t *testing.T, sc config.SwarmConfig, sim *service.Simulator) (*service.Node, *Discovery) {
	ln, ninfo := node.GenerateTestNode(t)
	n := sim.NewNodeFrom(ninfo)
	dht := New(context.TODO(), ln, sc, n, "", logtest.New(t).WithName("dhttest"+uuid.New().String()))
	// n.AttachDHT(discovery)

	return n, dht
}

func TestDHT_BootstrapAbort(t *testing.T) {
	// Create a bootstrap node
	sim := service.NewSimulator()
	bn, bdht := simNodeWithDHT(t, config.DefaultConfig().SwarmConfig, sim)
	t.Cleanup(func() {
		bdht.Shutdown()
	})
	// config for other nodes
	cfg2 := config.DefaultConfig()
	cfg2.SwarmConfig.RandomConnections = 2
	cfg2.SwarmConfig.BootstrapNodes = []string{bn.String()}
	_, dht := simNodeWithDHT(t, cfg2.SwarmConfig, sim)
	t.Cleanup(func() {
		dht.Shutdown()
	})
	// Create a bootstrap node to abort
	Ctx, Cancel := context.WithCancel(context.Background())
	// Abort bootstrap after 500 milliseconds
	Cancel()
	// Should return error after 2 seconds
	err := dht.Bootstrap(Ctx)
	require.EqualError(t, err, ErrBootAbort.Error(), "Should be able to abort bootstrap")
}

func TestKadDHT_VerySmallBootstrap(t *testing.T) {
	connections := 1

	bncfg := config.DefaultConfig()
	sim := service.NewSimulator()

	bn, bninfo := node.GenerateTestNode(t)
	b1 := sim.NewNodeFrom(bninfo)
	bdht := New(context.TODO(), bn, bncfg.SwarmConfig, b1, "", logtest.New(t).WithName(t.Name()+"_"+bninfo.PublicKey().String()))
	t.Cleanup(func() {
		bdht.Shutdown()
	})
	t.Cleanup(func() {
		bdht.Shutdown()
	})

	bdht.SetLocalAddresses(int(bninfo.ProtocolPort), int(bninfo.DiscoveryPort))

	extra, extrainfo := node.GenerateTestNode(t)
	extrasvc := sim.NewNodeFrom(extrainfo)
	edht := New(context.TODO(), extra, bncfg.SwarmConfig, extrasvc, "", logtest.New(t).WithName(t.Name()+"_"+extra.PublicKey().String()))
	t.Cleanup(func() {
		edht.Shutdown()
	})

	edht.SetLocalAddresses(int(bninfo.ProtocolPort), int(bninfo.DiscoveryPort))
	edht.rt.AddAddress(generateDiscNode(), extrainfo)

	bdht.rt.AddAddress(extrainfo, bninfo)

	cfg := config.DefaultConfig().SwarmConfig
	cfg.Gossip = false
	cfg.Bootstrap = true
	cfg.RandomConnections = connections
	// cfg.RoutingTableBucketSize = 2
	cfg.BootstrapNodes = append(cfg.BootstrapNodes, bninfo.String())

	ln, lninfo := node.GenerateTestNode(t)
	n := sim.NewNodeFrom(lninfo)
	dht := New(context.TODO(), ln, cfg, n, "", logtest.New(t).WithName(t.Name()+lninfo.PublicKey().String()))
	t.Cleanup(func() {
		dht.Shutdown()
	})
	dht.SetLocalAddresses(int(lninfo.ProtocolPort), int(lninfo.DiscoveryPort))
	err := dht.Bootstrap(context.TODO())

	require.NoError(t, err)

	res, err := bdht.rt.Lookup(ln.PublicKey())
	require.NoError(t, err)

	require.Equal(t, res.ID, lninfo.ID)
	require.Equal(t, res.DiscoveryPort, lninfo.DiscoveryPort)
	require.Equal(t, res.ProtocolPort, lninfo.ProtocolPort)

	res2, _ := dht.rt.Lookup(bn.PublicKey())
	// require.Error(t, err2)
	require.NotEqual(t, res2, bn)
	// bootstrap nodes are removed at the end of bootstrap
}

func TestKadDHT_BootstrapSingleBoot(t *testing.T) {
	numPeers := 100

	bncfg := config.DefaultConfig()
	sim := service.NewSimulator()

	bn, bninfo := node.GenerateTestNode(t)
	b1 := sim.NewNodeFrom(bninfo)
	b1Dht := New(context.TODO(), bn, bncfg.SwarmConfig, b1, "", logtest.New(t).WithName(t.Name()+"_"+bninfo.String()))
	t.Cleanup(func() {
		b1Dht.Shutdown()
	})

	_ = New(context.TODO(), bn, bncfg.SwarmConfig, b1, "", logtest.New(t).WithName(t.Name()+"_"+bninfo.String()))

	cfg := config.DefaultConfig().SwarmConfig
	cfg.Gossip = false
	cfg.Bootstrap = true
	cfg.BootstrapNodes = append(cfg.BootstrapNodes, bninfo.String())
	cfg.RandomConnections = 8

	donech := make(chan struct{}, numPeers)

	nods, dhts := make([]*node.Info, numPeers), make([]*Discovery, numPeers)

	for i := 0; i < numPeers; i++ {
		ln, lninfo := node.GenerateTestNode(t)
		n := sim.NewNodeFrom(lninfo)
		// dht := New(context.TODO(), ln, cfg, n, "", logtest.New(t, zapcore.InfoLevel).WithName("dht"+strconv.Itoa(i)))
		dht := New(context.TODO(), ln, cfg, n, "", logtest.New(t).WithName("dht"+strconv.Itoa(i)))
		dht.SetLocalAddresses(int(lninfo.ProtocolPort), int(lninfo.DiscoveryPort))
		nods[i] = lninfo
		dhts[i] = dht
		go func() {
			err := dht.Bootstrap(context.TODO())
			require.NoError(t, err)
			donech <- struct{}{}
		}()
	}
	t.Cleanup(func() {
		for _, dht := range dhts {
			dht.Shutdown()
		}
	})

	tm := time.NewTimer(tstBootstrapTimeout)

	for i := 0; i < numPeers; i++ {
		select {
		case <-donech:
		case <-tm.C:
			t.Fatal("didn't boot successfully")
		}
	}

	testTables(t, dhts, 8, 10)
}

func TestKadDHT_Bootstrap(t *testing.T) {
	numPeers := 1500
	min := 8
	bootnum := 5

	sim := service.NewSimulator()

	bncfg := config.DefaultConfig()

	cfg := config.DefaultConfig().SwarmConfig
	cfg.Gossip = false
	cfg.Bootstrap = true

	bDhts := make([]*Discovery, bootnum)
	for b := 0; b < bootnum; b++ {
		bn, bninfo := node.GenerateTestNode(t)
		b1 := sim.NewNodeFrom(bninfo)
		disc := New(context.TODO(), bn, bncfg.SwarmConfig, b1, "", logtest.New(t).WithName("bn"+strconv.Itoa(b)))
		bDhts[b] = disc
		disc.SetLocalAddresses(int(bninfo.ProtocolPort), int(bninfo.DiscoveryPort))
		cfg.BootstrapNodes = append(cfg.BootstrapNodes, bninfo.String())
	}
	t.Cleanup(func() {
		for _, bDht := range bDhts {
			bDht.Shutdown()
		}
	})

	cfg.RandomConnections = min

	donech := make(chan struct{}, numPeers)

	nods, dhts := make([]*node.Info, numPeers), make([]*Discovery, numPeers)

	for i := 0; i < numPeers; i++ {
		ln, lninfo := node.GenerateTestNode(t)
		n := sim.NewNodeFrom(lninfo)
		dht := New(context.TODO(), ln, cfg, n, "", logtest.New(t).WithName("dht"+strconv.Itoa(i)))
		nods[i] = lninfo
		dhts[i] = dht
		go func() {
			err := dht.Bootstrap(context.TODO())
			require.NoError(t, err)
			donech <- struct{}{}
		}()
	}
	t.Cleanup(func() {
		for _, dht := range dhts {
			dht.Shutdown()
		}
	})

	tm := time.NewTimer(tstBootstrapTimeout)

	for i := 0; i < numPeers; i++ {
		select {
		case <-donech:
		case <-tm.C:
			t.Fatal("didn't boot successfully")
		}
	}

	testTables(t, dhts, 8, 10)
}

func testTables(t *testing.T, dhts []*Discovery, min, avg int) {
	all := 0
	for i, dht := range dhts {
		size := dht.rt.NumAddresses()
		all += size
		if min > 0 && size < min {
			t.Fatalf("discovery %d (%v) has %d peers min is %d", i, dht.local.PublicKey().String(), size, min)
		}
	}
	avgSize := all / len(dhts)
	if avg > 0 && avgSize < avg {
		t.Fatalf("avg rt size is %d, was expecting %d", avgSize, avg)
	}
}

func Test_findNodeFailure(t *testing.T) {
	sim := service.NewSimulator()

	bsnode, bsinfo := node.GenerateTestNode(t)

	cfg := config.DefaultConfig().SwarmConfig
	cfg.RandomConnections = 1
	cfg.RoutingTableBucketSize = 1
	cfg.BootstrapNodes = []string{bsinfo.String()}
	_, dht2 := simNodeWithDHT(t, cfg, sim)
	t.Cleanup(func() {
		dht2.Shutdown()
	})

	var dht *Discovery
	go func() {
		<-time.After(time.Second)
		realnode := sim.NewNodeFrom(bsinfo)
		dht = New(context.TODO(), bsnode, config.DefaultConfig().SwarmConfig, realnode, "", logtest.New(t).WithName(t.Name()))
		t.Cleanup(func() {
			dht.Shutdown()
		})
		<-time.After(time.Second)
		nd, _ := simNodeWithDHT(t, config.DefaultConfig().SwarmConfig, sim)
		dht.rt.AddAddress(nd.Info, bsinfo)
	}()

	err := dht2.Bootstrap(context.TODO())
	require.NoError(t, err)
	sz := dht2.Size()
	require.Equal(t, sz, 1)
}

func Test_Refresh(t *testing.T) {
	sim := service.NewSimulator()
	bsnode, bsinfo := node.GenerateTestNode(t)
	serv := sim.NewNodeFrom(bsinfo)

	disc := New(context.TODO(), bsnode, config.DefaultConfig().SwarmConfig, serv, "", logtest.New(t).WithName(""))
	t.Cleanup(func() {
		disc.Shutdown()
	})
	rt := &mockAddrBook{}
	rt.NeedNewAddressesFunc = func() bool {
		return true
	}

	requsted := 0

	refresher := &refresherMock{}

	refresher.BootstrapFunc = func(ctx context.Context, minPeers int) error {
		requsted = minPeers
		return nil
	}

	disc.rt = rt
	disc.bootstrapper = refresher

	prz := disc.SelectPeers(context.TODO(), 10)
	require.Len(t, prz, 0)
	require.Equal(t, requsted, 10)

	requsted = 0

	rt.NeedNewAddressesFunc = func() bool {
		return false
	}

	prz = disc.SelectPeers(context.TODO(), 10)
	require.Len(t, prz, 0)
	require.Equal(t, requsted, 0)
}
