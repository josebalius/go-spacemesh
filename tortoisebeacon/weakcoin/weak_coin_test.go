package weakcoin_test

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"math/rand"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	servicemocks "github.com/spacemeshos/go-spacemesh/p2p/service/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	smocks "github.com/spacemeshos/go-spacemesh/signing/mocks"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/weakcoin"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/weakcoin/mocks"
	"github.com/stretchr/testify/require"
)

func noopBroadcaster(tb testing.TB, ctrl *gomock.Controller) *mocks.Mockbroadcaster {
	bc := mocks.NewMockbroadcaster(ctrl)
	bc.EXPECT().Broadcast(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
	return bc
}

func broadcastedMessage(tb testing.TB, ctrl *gomock.Controller, msg weakcoin.Message) *servicemocks.MockGossipMessage {
	tb.Helper()
	mm := servicemocks.NewMockGossipMessage(ctrl)
	mm.EXPECT().Sender().Return(p2pcrypto.NewRandomPubkey()).AnyTimes()
	buf, err := types.InterfaceToBytes(&msg)
	require.NoError(tb, err)
	mm.EXPECT().Bytes().Return(buf).AnyTimes()
	mm.EXPECT().ReportValidation(gomock.Any(), gomock.Any()).AnyTimes()
	return mm
}

func staticSigner(tb testing.TB, ctrl *gomock.Controller, sig []byte) *smocks.MockSigner {
	tb.Helper()
	signer := smocks.NewMockSigner(ctrl)
	signer.EXPECT().Sign(gomock.Any()).Return(sig).AnyTimes()
	signer.EXPECT().PublicKey().Return(signing.NewPublicKey(sig)).AnyTimes()
	signer.EXPECT().LittleEndian().Return(true).AnyTimes()
	return signer
}

func sigVerifier(tb testing.TB, ctrl *gomock.Controller) *smocks.MockVerifier {
	tb.Helper()
	verifier := smocks.NewMockVerifier(ctrl)
	verifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	return verifier
}

func TestWeakCoin(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	var (
		epoch types.EpochID = 10
		round types.RoundID = 4

		oneLSB          = []byte{0b0001}
		zeroLSB         = []byte{0b0110}
		higherThreshold = []byte{0xff}

		verifier = sigVerifier(t, ctrl)
	)

	tcs := []struct {
		desc         string
		local        []byte
		allowances   weakcoin.UnitAllowances
		startedEpoch types.EpochID
		startedRound types.RoundID
		messages     []weakcoin.Message
		coinflip     bool
	}{
		{
			desc:         "ValidProposalFromNetwork",
			local:        zeroLSB,
			allowances:   weakcoin.UnitAllowances{string(oneLSB): 1, string(zeroLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:     epoch,
				Round:     round,
				Unit:      1,
				MinerPK:   oneLSB,
				Signature: oneLSB,
			}},
			coinflip: true,
		},
		{
			desc:         "LocalProposer",
			local:        zeroLSB,
			allowances:   weakcoin.UnitAllowances{string(oneLSB): 1, string(zeroLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
		},
		{
			desc:         "ProposalFromNetworkNotAllowed",
			local:        zeroLSB,
			allowances:   weakcoin.UnitAllowances{string(oneLSB): 1, string(zeroLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:     epoch,
				Round:     round,
				Unit:      2,
				MinerPK:   oneLSB,
				Signature: oneLSB,
			}},
		},
		{
			desc:         "ProposalFromNetworkHigherThreshold",
			local:        zeroLSB,
			allowances:   weakcoin.UnitAllowances{string(higherThreshold): 1, string(zeroLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:     epoch,
				Round:     round,
				Unit:      1,
				MinerPK:   higherThreshold,
				Signature: higherThreshold,
			}},
		},
		{
			desc:         "LocalProposalHigherThreshold",
			local:        higherThreshold,
			allowances:   weakcoin.UnitAllowances{string(higherThreshold): 1, string(zeroLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:     epoch,
				Round:     round,
				Unit:      1,
				MinerPK:   zeroLSB,
				Signature: zeroLSB,
			}},
		},
		{
			desc:         "LocalProposalNotAllowed",
			local:        oneLSB,
			allowances:   weakcoin.UnitAllowances{string(zeroLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:     epoch,
				Round:     round,
				Unit:      1,
				MinerPK:   zeroLSB,
				Signature: zeroLSB,
			}},
		},
		{
			desc:         "PreviousEpoch",
			local:        zeroLSB,
			allowances:   weakcoin.UnitAllowances{string(zeroLSB): 1, string(oneLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:     epoch - 1,
				Round:     round,
				Unit:      1,
				MinerPK:   zeroLSB,
				Signature: oneLSB,
			}},
		},
		{
			desc:         "PreviousRound",
			local:        zeroLSB,
			allowances:   weakcoin.UnitAllowances{string(zeroLSB): 1, string(oneLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:     epoch,
				Round:     round - 1,
				Unit:      1,
				MinerPK:   zeroLSB,
				Signature: oneLSB,
			}},
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			wc := weakcoin.New(
				noopBroadcaster(t, ctrl),
				staticSigner(t, ctrl, tc.local), verifier,
				weakcoin.WithThreshold([]byte{0xfe}),
			)

			wc.StartEpoch(context.TODO(), tc.startedEpoch, tc.allowances)
			require.NoError(t, wc.StartRound(context.TODO(), tc.startedRound))

			for _, msg := range tc.messages {
				wc.HandleSerializedMessage(context.TODO(), broadcastedMessage(t, ctrl, msg), nil)
			}
			wc.FinishRound(context.TODO())

			require.Equal(t, tc.coinflip, wc.Get(context.TODO(), tc.startedEpoch, tc.startedRound))
		})
	}

	for _, tc := range tcs {
		tc := tc
		t.Run("BufferingStartEpoch/"+tc.desc, func(t *testing.T) {
			wc := weakcoin.New(
				noopBroadcaster(t, ctrl),
				staticSigner(t, ctrl, tc.local), verifier,
				weakcoin.WithThreshold([]byte{0xfe}),
			)

			wc.StartEpoch(context.TODO(), tc.startedEpoch, tc.allowances)

			for _, msg := range tc.messages {
				wc.HandleSerializedMessage(context.TODO(), broadcastedMessage(t, ctrl, msg), nil)
			}
			require.NoError(t, wc.StartRound(context.TODO(), tc.startedRound))
			wc.FinishRound(context.TODO())

			require.Equal(t, tc.coinflip, wc.Get(context.TODO(), tc.startedEpoch, tc.startedRound))
		})
	}

	for _, tc := range tcs {
		tc := tc
		t.Run("BufferingNextRound/"+tc.desc, func(t *testing.T) {
			wc := weakcoin.New(
				noopBroadcaster(t, ctrl),
				staticSigner(t, ctrl, tc.local), verifier,
				weakcoin.WithThreshold([]byte{0xfe}),
			)

			wc.StartEpoch(context.TODO(), tc.startedEpoch, tc.allowances)
			require.NoError(t, wc.StartRound(context.TODO(), tc.startedRound))

			for _, msg := range tc.messages {
				msg.Round++
				wc.HandleSerializedMessage(context.TODO(), broadcastedMessage(t, ctrl, msg), nil)
			}

			require.NoError(t, wc.StartRound(context.TODO(), tc.startedRound+1))
			wc.FinishRound(context.TODO())

			require.Equal(t, tc.coinflip, wc.Get(context.TODO(), tc.startedEpoch, tc.startedRound+1))
			wc.FinishEpoch(context.TODO(), tc.startedEpoch)
		})
	}
	for _, tc := range tcs {
		tc := tc
		t.Run("BufferingNextEpochAfterCompletion/"+tc.desc, func(t *testing.T) {
			wc := weakcoin.New(
				noopBroadcaster(t, ctrl),
				staticSigner(t, ctrl, tc.local), verifier,
				weakcoin.WithThreshold([]byte{0xfe}),
			)

			wc.StartEpoch(context.TODO(), tc.startedEpoch, tc.allowances)
			wc.FinishEpoch(context.TODO(), tc.startedEpoch)

			wc.StartEpoch(context.TODO(), tc.startedEpoch+1, tc.allowances)
			for _, msg := range tc.messages {
				msg.Epoch++
				wc.HandleSerializedMessage(context.TODO(), broadcastedMessage(t, ctrl, msg), nil)
			}

			require.NoError(t, wc.StartRound(context.TODO(), tc.startedRound))
			wc.FinishRound(context.TODO())

			require.Equal(t, tc.coinflip, wc.Get(context.TODO(), tc.startedEpoch+1, tc.startedRound))
			wc.FinishEpoch(context.TODO(), tc.startedEpoch+1)
		})
	}
}

func TestWeakCoinGetPanic(t *testing.T) {
	var (
		ctrl = gomock.NewController(t)
		wc   = weakcoin.New(
			noopBroadcaster(t, ctrl),
			staticSigner(t, ctrl, []byte{1}), sigVerifier(t, ctrl))
		epoch types.EpochID = 10
		round types.RoundID = 2
	)
	defer ctrl.Finish()

	require.Panics(t, func() {
		wc.Get(context.TODO(), epoch, round)
	})

	wc.StartEpoch(context.TODO(), epoch, nil)
	require.False(t, wc.Get(context.TODO(), epoch, round))
}

func TestWeakCoinNextRoundBufferOverflow(t *testing.T) {
	var (
		ctrl = gomock.NewController(t)

		oneLSB  = []byte{0b0001}
		zeroLSB = []byte{0b0000}

		epoch     types.EpochID = 10
		round     types.RoundID = 2
		nextRound               = round + 1
		bufSize                 = 10
	)
	defer ctrl.Finish()

	wc := weakcoin.New(
		noopBroadcaster(t, ctrl),
		staticSigner(t, ctrl, oneLSB), sigVerifier(t, ctrl),
		weakcoin.WithNextRoundBufferSize(bufSize),
	)

	wc.StartEpoch(context.TODO(), epoch, weakcoin.UnitAllowances{string(oneLSB): 1, string(zeroLSB): 1})
	require.NoError(t, wc.StartRound(context.TODO(), round))
	for i := 0; i < bufSize; i++ {
		wc.HandleSerializedMessage(context.TODO(), broadcastedMessage(t, ctrl, weakcoin.Message{
			Epoch:     epoch,
			Round:     nextRound,
			Unit:      1,
			MinerPK:   oneLSB,
			Signature: oneLSB,
		}), nil)
	}
	wc.HandleSerializedMessage(context.TODO(), broadcastedMessage(t, ctrl, weakcoin.Message{
		Epoch:     epoch,
		Round:     nextRound,
		Unit:      1,
		Signature: zeroLSB,
	}), nil)
	wc.FinishRound(context.TODO())
	require.NoError(t, wc.StartRound(context.TODO(), nextRound))
	wc.FinishRound(context.TODO())
	require.True(t, wc.Get(context.TODO(), epoch, nextRound))
}

func TestWeakCoinEncodingRegression(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	var (
		sig   []byte
		epoch types.EpochID = 1
		round types.RoundID = 1
	)
	broadcaster := mocks.NewMockbroadcaster(ctrl)
	broadcaster.EXPECT().Broadcast(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(_ context.Context, _ string, data []byte) error {
		msg := weakcoin.Message{}
		require.NoError(t, types.BytesToInterface(data, &msg))
		sig = msg.Signature
		return nil
	}).AnyTimes()
	seed := make([]byte, ed25519.SeedSize)
	r := rand.New(rand.NewSource(999))
	r.Read(seed)
	signer, _, err := signing.NewVRFSigner(seed)
	require.NoError(t, err)

	allowances := weakcoin.UnitAllowances{string(signer.PublicKey().Bytes()): 1}
	instance := weakcoin.New(broadcaster, signer, signing.VRFVerifier{}, weakcoin.WithThreshold([]byte{0xff}))
	instance.StartEpoch(context.TODO(), epoch, allowances)
	require.NoError(t, instance.StartRound(context.TODO(), round))

	require.Equal(t,
		"a1f2c99f9210b15b66197fbc6f0dd5a93bfb08da63eae81b84d550cf5a6daf7e0c8e79fe3fefeac839bdce2de4f3cc3d420a8f43a9275bfed0221e99e3a4b204",
		util.Bytes2Hex(sig))
}

func TestWeakCoinExchangeProposals(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	var (
		instances                          = make([]*weakcoin.WeakCoin, 10)
		verifier                           = signing.VRFVerifier{}
		epochStart, epochEnd types.EpochID = 2, 6
		start, end           types.RoundID = 0, 9
		allowances                         = weakcoin.UnitAllowances{}
		r                                  = rand.New(rand.NewSource(999))
	)

	for i := range instances {
		i := i
		broadcaster := mocks.NewMockbroadcaster(ctrl)
		broadcaster.EXPECT().Broadcast(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().
			DoAndReturn(func(_ context.Context, _ string, data []byte) error {
				msg := weakcoin.Message{}
				require.NoError(t, types.BytesToInterface(data, &msg))
				for j := range instances {
					if i == j {
						continue
					}
					instances[j].HandleSerializedMessage(context.TODO(), broadcastedMessage(t, ctrl, msg), nil)
				}
				return nil
			}).AnyTimes()
		seed := make([]byte, ed25519.SeedSize)

		r.Read(seed)
		signer, _, err := signing.NewVRFSigner(seed)
		require.NoError(t, err)
		allowances[string(signer.PublicKey().Bytes())] = 1
		instances[i] = weakcoin.New(broadcaster, signer, verifier,
			weakcoin.WithLog(logtest.New(t).Named(fmt.Sprintf("coin=%d", i))))
	}

	for epoch := epochStart; epoch <= epochEnd; epoch++ {
		for _, instance := range instances {
			instance.StartEpoch(context.TODO(), epoch, allowances)
		}
		for current := start; current <= end; current++ {
			for _, instance := range instances {
				require.NoError(t, instance.StartRound(context.TODO(), current))
			}
			for _, instance := range instances {
				instance.FinishRound(context.TODO())
			}
			rst := instances[0].Get(context.TODO(), epoch, current)
			for _, instance := range instances[1:] {
				require.Equal(t, rst, instance.Get(context.TODO(), epoch, current), "round %d", current)
			}
		}
		for _, instance := range instances {
			instance.FinishEpoch(context.TODO(), epoch)
		}
	}
}
