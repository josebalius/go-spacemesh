package main

import (
	"context"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"time"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/p2p"
	node2 "github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon"
	"github.com/spacemeshos/post/initialization"
	"github.com/testground/sdk-go/sync"

	"github.com/spacemeshos/go-spacemesh/config"

	"github.com/spacemeshos/go-spacemesh/cmd/node"
	"github.com/spacemeshos/go-spacemesh/plans/network/poet"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

/*
// default poets=1 default gateways = 1
testground run single --plan=plans/network --testcase=start_network --builder=docker:generic --runner=local:docker --instances=5
*/

var testcases = map[string]interface{}{
	"start_network": run.InitializedTestCaseFn(StartNetwork),
}

// StartNetwork creates a basic spacemesh layout of instances.
// 	it first creates poet instances shares their addresses with gateways and miners
// 	gateway nodes function as bootstrap nodes and gateways for poets, their addresses
//  are distributed to miners.
//
// 	gateways - the number of bootstrap nodes and poet gateways
// 	poets - the number of poets
// 	the rest of the instances are miners
func StartNetwork(env *runtime.RunEnv, initCtx *run.InitContext) error {
	// TODO: extract a lot of constants/params

	poets_topic := sync.NewTopic("poets", string(""))
	gateways_topic := sync.NewTopic("gateways", node2.Info{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	client := initCtx.SyncClient
	netclient := initCtx.NetClient
	// role-allocation assigns a sequence number to each instance and distributes roles
	poets := env.IntParam("poets")
	gateways := env.IntParam("gateways")

	if err := netclient.WaitNetworkInitialized(ctx); err != nil {
		env.RecordFailure(err)
	}

	ra := &roleAllocator{}

	ra.Add("poet", poets, func(r *role) {

		poet_ip := netclient.MustGetDataNetworkIP()
		_, err := poet.StartPoet(func(c *poet.Config) {
			//['--rpclisten', '0.0.0.0:50002', '--restlisten', '0.0.0.0:80', "--n", "19"
			c.RawRPCListener = fmt.Sprintf("%v:50002", poet_ip.String())
			c.RawRESTListener = fmt.Sprintf("%v:80", poet_ip.String())
			c.Service.N = 19
		})
		if err != nil {
			env.RecordFailure(err)
		}
		poetHarness := activation.NewHTTPPoetClient("0.0.0.0:80")
		client.MustPublish(ctx, poets_topic, string(poet_ip.String()+":80"))
		env.RecordMessage("Published poet data ", string(poet_ip.String()+":80"))

		gatewaych := make(chan node2.Info)
		client.MustSubscribe(ctx, gateways_topic, gatewaych)
		gatewaylist := make([]string, 0)

		for g := range gatewaych {
			gatewaylist = append(gatewaylist, g.IP.String()+":9092")
		}
		if err := poetHarness.Start(ctx, gatewaylist); err != nil {
			env.RecordFailure(err)
		}

	}, func(obj interface{}) {
		env.RecordMessage("done starting poet %v", obj.(string))
	})

	ra.Add("gateway", gateways, func(r *role) {
		env.RecordMessage("hello im gateway")
		nd, err := InitNode(ctx)

		if err != nil {
			env.RecordFailure(err)
			return
		}

		env.RecordMessage("gateway waiting for poet message")
		poetch := make(chan string)
		client.MustSubscribe(ctx, poets_topic, poetch)
		poetaddr := <-poetch

		nd.Config.PoETServer = poetaddr

		go func(app *node.App) {
			err := app.Start()
			if err != nil {
				env.RecordFailure(err)
				return
			}
		}(nd)

		for nd.P2P == nil {
			time.Sleep(1 * time.Second)
		}
		info := nd.P2P.(*p2p.Switch).LocalNode()
		env.RecordMessage("gateway P2P is ready, p2pid :", info.PublicKey().String())

		ip := netclient.MustGetDataNetworkIP()
		port := uint16(7513)
		bs_info := node2.NewNode(info.PublicKey(), ip, port, port)

		client.MustPublish(ctx, gateways_topic, bs_info)

		r.storage = string("LOL")
	}, func(obj interface{}) {
		env.RecordMessage("done gateways %v", obj.(string))
	})

	ra.Add("miner", env.TestInstanceCount-poets-gateways, func(r *role) {
		env.RecordMessage("hello im miner")
		r.storage = string("LOL")
	}, func(obj interface{}) {
		env.RecordMessage("done miner %v", obj.(string))
	})

	seq := int(client.MustSignalAndWait(ctx, "role-allocation", env.TestInstanceCount))

	if err := ra.Allocate(seq); err != nil {
		env.RecordFailure(err)
	}

	<-ctx.Done()
	return nil
}

func createMinerConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.POST = activation.DefaultPostConfig()
	cfg.POST.LabelsPerUnit = 32
	cfg.POST.BitsPerLabel = 8
	cfg.POST.K2 = 4

	cfg.SMESHING = config.DefaultSmeshingConfig()
	cfg.SMESHING.Start = true
	cfg.SMESHING.Opts.NumUnits = cfg.POST.MinNumUnits + 1
	cfg.SMESHING.Opts.NumFiles = 1
	cfg.SMESHING.Opts.ComputeProviderID = int(initialization.CPUProviderID())

	cfg.HARE.N = 800
	cfg.HARE.F = 399
	cfg.HARE.RoundDuration = 7
	cfg.HARE.WakeupDelta = 20
	cfg.HARE.ExpectedLeaders = 10
	//cfg.HARE.SuperHare = true
	cfg.LayerAvgSize = 50
	cfg.LayersPerEpoch = 3
	cfg.TxsPerBlock = 100
	cfg.Hdist = 5

	cfg.LayerDurationSec = 60
	cfg.HareEligibility.ConfidenceParam = 6
	cfg.HareEligibility.EpochOffset = 0
	cfg.SyncRequestTimeout = 10000
	//cfg.SyncInterval = 2
	//cfg.SyncValidationDelta = 5
	//
	//cfg.FETCH.RequestTimeout = 10
	//cfg.FETCH.MaxRetiresForPeer = 5
	//cfg.FETCH.BatchSize = 5
	//cfg.FETCH.BatchTimeout = 5
	//
	cfg.LAYERS.RequestTimeout = 10
	cfg.SMESHING.CoinbaseAccount = "0x123"
	cfg.GoldenATXID = "0x5678"

	cfg.LOGGING.AppLoggerLevel = "info"
	cfg.LOGGING.P2PLoggerLevel = "info"
	cfg.LOGGING.PostLoggerLevel = "info"
	cfg.LOGGING.StateDbLoggerLevel = "info"
	cfg.LOGGING.StateLoggerLevel = "info"
	cfg.LOGGING.AtxDbStoreLoggerLevel = "info"
	cfg.LOGGING.TBeaconDbStoreLoggerLevel = "info"
	cfg.LOGGING.TBeaconDbLoggerLevel = "info"
	cfg.LOGGING.TBeaconLoggerLevel = "info"
	cfg.LOGGING.WeakCoinLoggerLevel = "info"
	cfg.LOGGING.PoetDbStoreLoggerLevel = "info"
	cfg.LOGGING.StoreLoggerLevel = "info"
	cfg.LOGGING.PoetDbLoggerLevel = "info"
	cfg.LOGGING.MeshDBLoggerLevel = "info"
	cfg.LOGGING.TrtlLoggerLevel = "info"
	cfg.LOGGING.AtxDbLoggerLevel = "info"
	cfg.LOGGING.BlkEligibilityLoggerLevel = "info"
	cfg.LOGGING.MeshLoggerLevel = "info"
	cfg.LOGGING.SyncLoggerLevel = "info"
	cfg.LOGGING.BlockOracleLevel = "info"
	cfg.LOGGING.HareOracleLoggerLevel = "info"
	cfg.LOGGING.HareLoggerLevel = "info"
	cfg.LOGGING.BlockBuilderLoggerLevel = "info"
	cfg.LOGGING.BlockListenerLoggerLevel = "info"
	cfg.LOGGING.PoetListenerLoggerLevel = "info"
	cfg.LOGGING.NipostBuilderLoggerLevel = "info"
	cfg.LOGGING.AtxBuilderLoggerLevel = "info"
	cfg.LOGGING.HareBeaconLoggerLevel = "info"
	cfg.LOGGING.TimeSyncLoggerLevel = "info"

	cfg.TortoiseBeacon = tortoisebeacon.DefaultConfig()
	return &cfg
}


func InitNode(ctx context.Context) (*node.App, error) {
	nd := node.New(node.WithConfig(createMinerConfig()),
		node.WithLog(log.RegisterHooks(
			log.NewWithLevel("", zap.NewAtomicLevelAt(zapcore.DebugLevel)),
			events.EventHook())),
	)
	if err := nd.Initialize(); err != nil {
		return nil, err
	}
	return nd, nil
}


func main() {
	run.InvokeMap(testcases)
}
