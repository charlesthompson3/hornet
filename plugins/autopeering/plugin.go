package autopeering

import (
	"context"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p-core/crypto"
	libp2p "github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"go.uber.org/dig"

	databaseCore "github.com/gohornet/hornet/core/database"
	"github.com/gohornet/hornet/core/gossip"
	"github.com/gohornet/hornet/core/pow"
	"github.com/gohornet/hornet/core/snapshot"
	"github.com/gohornet/hornet/core/tangle"
	"github.com/gohornet/hornet/pkg/daemon"
	"github.com/gohornet/hornet/pkg/database"
	"github.com/gohornet/hornet/pkg/p2p"
	"github.com/gohornet/hornet/pkg/p2p/autopeering"
	"github.com/gohornet/hornet/plugins/dashboard"
	"github.com/gohornet/hornet/plugins/debug"
	"github.com/gohornet/hornet/plugins/inx"
	"github.com/gohornet/hornet/plugins/prometheus"
	"github.com/gohornet/hornet/plugins/receipt"
	restapiv2 "github.com/gohornet/hornet/plugins/restapi/v2"
	"github.com/gohornet/hornet/plugins/spammer"
	"github.com/gohornet/hornet/plugins/urts"
	"github.com/gohornet/hornet/plugins/warpsync"
	"github.com/iotaledger/hive.go/app"
	"github.com/iotaledger/hive.go/autopeering/discover"
	"github.com/iotaledger/hive.go/autopeering/peer/service"
	"github.com/iotaledger/hive.go/autopeering/selection"
	"github.com/iotaledger/hive.go/crypto/ed25519"
	"github.com/iotaledger/hive.go/events"
	iotago "github.com/iotaledger/iota.go/v3"
)

func init() {
	Plugin = &app.Plugin{
		Status: app.StatusDisabled,
		Component: &app.Component{
			Name:       "Autopeering",
			DepsFunc:   func(cDeps dependencies) { deps = cDeps },
			Params:     params,
			PreProvide: preProvide,
			Provide:    provide,
			Configure:  configure,
			Run:        run,
		},
	}
}

var (
	Plugin *app.Plugin
	deps   dependencies

	localPeerContainer *autopeering.LocalPeerContainer

	onDiscoveryPeerDiscovered  *events.Closure
	onDiscoveryPeerDeleted     *events.Closure
	onSelectionSaltUpdated     *events.Closure
	onSelectionOutgoingPeering *events.Closure
	onSelectionIncomingPeering *events.Closure
	onSelectionDropped         *events.Closure
	onPeerDisconnected         *events.Closure
	onAutopeerBecameKnown      *events.Closure
)

type dependencies struct {
	dig.In
	NodePrivateKey            crypto.PrivKey  `name:"nodePrivateKey"`
	P2PDatabasePath           string          `name:"p2pDatabasePath"`
	P2PBindMultiAddresses     []string        `name:"p2pBindMultiAddresses"`
	DatabaseEngine            database.Engine `name:"databaseEngine"`
	AutopeeringRunAsEntryNode bool            `name:"autopeeringRunAsEntryNode"`
	PeeringManager            *p2p.Manager    `optional:"true"`
	AutopeeringManager        *autopeering.AutopeeringManager
}

func preProvide(c *dig.Container, application *app.App, initConfig *app.InitConfig) error {

	pluginEnabled := true

	containsPlugin := func(pluginsList []string, pluginIdentifier string) bool {
		contains := false
		for _, plugin := range pluginsList {
			if strings.ToLower(plugin) == pluginIdentifier {
				contains = true
				break
			}
		}
		return contains
	}

	if disabled := containsPlugin(initConfig.DisabledPlugins, Plugin.Identifier()); disabled {
		// Autopeering is disabled
		pluginEnabled = false
	}

	if enabled := containsPlugin(initConfig.EnabledPlugins, Plugin.Identifier()); !enabled {
		// Autopeering was not enabled
		pluginEnabled = false
	}

	runAsEntryNode := pluginEnabled && ParamsAutopeering.RunAsEntryNode
	if runAsEntryNode {
		// the following pluggables stay enabled
		// - profile
		// - protocfg
		// - gracefulshutdown
		// - p2p
		// - profiling
		// - versioncheck
		// - autopeering

		// disable the other plugins if the node runs as an entry node for autopeering
		initConfig.ForceDisableComponent(databaseCore.CoreComponent.Identifier())
		initConfig.ForceDisableComponent(pow.CoreComponent.Identifier())
		initConfig.ForceDisableComponent(gossip.CoreComponent.Identifier())
		initConfig.ForceDisableComponent(tangle.CoreComponent.Identifier())
		initConfig.ForceDisableComponent(snapshot.CoreComponent.Identifier())
		initConfig.ForceDisableComponent(restapiv2.Plugin.Identifier())
		initConfig.ForceDisableComponent(warpsync.Plugin.Identifier())
		initConfig.ForceDisableComponent(urts.Plugin.Identifier())
		initConfig.ForceDisableComponent(dashboard.Plugin.Identifier())
		initConfig.ForceDisableComponent(spammer.Plugin.Identifier())
		initConfig.ForceDisableComponent(receipt.Plugin.Identifier())
		initConfig.ForceDisableComponent(prometheus.Plugin.Identifier())
		initConfig.ForceDisableComponent(inx.Plugin.Identifier())
		initConfig.ForceDisableComponent(debug.Plugin.Identifier())
	}

	// the parameter has to be provided in the preProvide stage.
	// this is a special case, since it only should be true if the plugin is enabled
	type cfgResult struct {
		dig.Out
		AutopeeringRunAsEntryNode bool `name:"autopeeringRunAsEntryNode"`
	}

	if err := c.Provide(func() cfgResult {
		return cfgResult{
			AutopeeringRunAsEntryNode: runAsEntryNode,
		}
	}); err != nil {
		Plugin.LogPanic(err)
	}

	return nil
}

func provide(c *dig.Container) error {

	type autopeeringDeps struct {
		dig.In
		ProtocolParameters *iotago.ProtocolParameters
	}

	if err := c.Provide(func(deps autopeeringDeps) *autopeering.AutopeeringManager {
		return autopeering.NewAutopeeringManager(
			Plugin.Logger(),
			ParamsAutopeering.BindAddress,
			ParamsAutopeering.EntryNodes,
			ParamsAutopeering.EntryNodesPreferIPv6,
			service.Key(deps.ProtocolParameters.NetworkName),
		)
	}); err != nil {
		Plugin.LogPanic(err)
	}

	return nil
}

func configure() error {
	selection.SetParameters(selection.Parameters{
		InboundNeighborSize:  ParamsAutopeering.InboundPeers,
		OutboundNeighborSize: ParamsAutopeering.OutboundPeers,
		SaltLifetime:         ParamsAutopeering.SaltLifetime,
	})

	if err := autopeering.RegisterAutopeeringProtocolInMultiAddresses(); err != nil {
		Plugin.LogPanicf("unable to register autopeering protocol for multi addresses: %s", err)
	}

	rawPrvKey, err := deps.NodePrivateKey.Raw()
	if err != nil {
		Plugin.LogPanicf("unable to obtain raw private key: %s", err)
	}

	localPeerContainer, err = autopeering.NewLocalPeerContainer(
		deps.AutopeeringManager.P2PServiceKey(),
		rawPrvKey[:ed25519.SeedSize],
		deps.P2PDatabasePath,
		deps.DatabaseEngine,
		deps.P2PBindMultiAddresses,
		ParamsAutopeering.BindAddress,
		deps.AutopeeringRunAsEntryNode,
	)
	if err != nil {
		Plugin.LogPanicf("unable to initialize local peer container: %s", err)
	}

	Plugin.LogInfof("initialized local autopeering: %s@%s", localPeerContainer.Local().PublicKey(), localPeerContainer.Local().Address())

	if deps.AutopeeringRunAsEntryNode {
		entryNodeMultiAddress, err := autopeering.GetEntryNodeMultiAddress(localPeerContainer.Local())
		if err != nil {
			Plugin.LogPanicf("unable to parse entry node multiaddress: %s", err)
		}

		Plugin.LogInfof("\n\nentry node multiaddress: %s\n", entryNodeMultiAddress.String())
	}

	// only enable peer selection when the peering plugin is enabled
	initSelection := deps.PeeringManager != nil

	deps.AutopeeringManager.Init(localPeerContainer, initSelection)
	configureEvents()

	return nil
}

func run() error {
	if err := Plugin.App.Daemon().BackgroundWorker(Plugin.Name, func(ctx context.Context) {
		attachEvents()
		deps.AutopeeringManager.Run(ctx)
		detachEvents()
	}, daemon.PriorityAutopeering); err != nil {
		Plugin.LogPanicf("failed to start worker: %s", err)
	}

	return nil
}

func configureEvents() {

	onDiscoveryPeerDiscovered = events.NewClosure(func(ev *discover.DiscoveredEvent) {
		peerID, err := autopeering.HivePeerToPeerID(ev.Peer)
		if err != nil {
			Plugin.LogWarnf("unable to convert discovered autopeering peer to peerID: %s", err)
			return
		}

		Plugin.LogInfof("discovered: %s / %s", ev.Peer.Address(), peerID.ShortString())
	})

	onDiscoveryPeerDeleted = events.NewClosure(func(ev *discover.DeletedEvent) {
		peerID, err := autopeering.HivePeerToPeerID(ev.Peer)
		if err != nil {
			Plugin.LogWarnf("unable to convert deleted autopeering peer to peerID: %s", err)
			return
		}

		Plugin.LogInfof("removed offline: %s / %s", ev.Peer.Address(), peerID.ShortString())
	})

	onPeerDisconnected = events.NewClosure(func(peerOptErr *p2p.PeerOptError) {
		if peerOptErr.Peer.Relation != p2p.PeerRelationAutopeered {
			return
		}

		if deps.AutopeeringManager.Selection() != nil {
			if id := autopeering.ConvertPeerIDToHiveIdentityOrLog(peerOptErr.Peer, Plugin.LogWarnf); id != nil {
				Plugin.LogInfof("removing: %s", peerOptErr.Peer.ID.ShortString())
				deps.AutopeeringManager.Selection().RemoveNeighbor(id.ID())
			}
		}
	})

	onAutopeerBecameKnown = events.NewClosure(func(p *p2p.Peer, oldRel p2p.PeerRelation) {
		if oldRel != p2p.PeerRelationAutopeered {
			return
		}
		if deps.AutopeeringManager.Selection() != nil {
			if id := autopeering.ConvertPeerIDToHiveIdentityOrLog(p, Plugin.LogWarnf); id != nil {
				Plugin.LogInfof("removing %s from autopeering selection protocol", p.ID.ShortString())
				deps.AutopeeringManager.Selection().RemoveNeighbor(id.ID())
			}
		}
	})

	onSelectionSaltUpdated = events.NewClosure(func(ev *selection.SaltUpdatedEvent) {
		Plugin.LogInfof("salt updated; expires=%s", ev.Public.GetExpiration().Format(time.RFC822))
	})

	onSelectionOutgoingPeering = events.NewClosure(func(ev *selection.PeeringEvent) {
		if !ev.Status {
			return
		}

		addrInfo, err := autopeering.HivePeerToAddrInfo(ev.Peer, deps.AutopeeringManager.P2PServiceKey())
		if err != nil {
			Plugin.LogWarnf("unable to convert outgoing selection autopeering peer to addr info: %s", err)
			return
		}

		Plugin.LogInfof("[outgoing peering] adding autopeering peer %s", addrInfo.ID.ShortString())

		handleSelection(ev, addrInfo, func() {
			Plugin.LogInfof("connecting to %s", addrInfo.ID.ShortString())
			if err := deps.PeeringManager.ConnectPeer(addrInfo, p2p.PeerRelationAutopeered); err != nil {
				Plugin.LogWarnf("couldn't add autopeering peer %s: %s", addrInfo.ID.ShortString(), err)
			}
		})
	})

	onSelectionIncomingPeering = events.NewClosure(func(ev *selection.PeeringEvent) {
		if !ev.Status {
			return
		}

		addrInfo, err := autopeering.HivePeerToAddrInfo(ev.Peer, deps.AutopeeringManager.P2PServiceKey())
		if err != nil {
			Plugin.LogWarnf("unable to convert incoming selection autopeering peer to addr info: %s", err)
			return
		}

		Plugin.LogInfof("[incoming peering] allow autopeering peer %s", addrInfo.ID.ShortString())

		handleSelection(ev, addrInfo, func() {
			if err := deps.PeeringManager.AllowPeer(addrInfo.ID); err != nil {
				Plugin.LogWarnf("couldn't allow autopeering peer %s: %s", addrInfo.ID.ShortString(), err)
			}
		})
	})

	onSelectionDropped = events.NewClosure(func(ev *selection.DroppedEvent) {
		peerID, err := autopeering.HivePeerToPeerID(ev.Peer)
		if err != nil {
			Plugin.LogWarnf("unable to convert dropped autopeering peer to peerID: %s", err)
			return
		}

		Plugin.LogInfof("[dropped event] disconnecting %s / %s", ev.Peer.Address(), peerID.ShortString())

		if err := deps.PeeringManager.DisallowPeer(peerID); err != nil {
			Plugin.LogWarnf("couldn't disallow autopeering peer %s: %s", peerID.ShortString(), err)
		}

		var peerRelation p2p.PeerRelation
		deps.PeeringManager.Call(peerID, func(p *p2p.Peer) {
			peerRelation = p.Relation
		})

		if len(peerRelation) == 0 {
			Plugin.LogWarnf("didn't find autopeered peer %s for disconnecting", peerID.ShortString())
			return
		}

		if peerRelation != p2p.PeerRelationAutopeered {
			Plugin.LogWarnf("won't disconnect %s as its relation is not '%s' but '%s'", peerID.ShortString(), p2p.PeerRelationAutopeered, peerRelation)
			return
		}

		if err := deps.PeeringManager.DisconnectPeer(peerID, errors.New("removed via autopeering selection")); err != nil {
			Plugin.LogWarnf("couldn't disconnect selection dropped autopeer %s: %s", peerID.ShortString(), err)
		}
	})
}

// handles a peer gotten from the autopeering selection according to its existing relation.
// if the peer is not yet part of the peering manager, the given noRelationFunc is called.
func handleSelection(ev *selection.PeeringEvent, addrInfo *libp2p.AddrInfo, noRelationFunc func()) {
	// extract peer relation
	var peerRelation p2p.PeerRelation
	deps.PeeringManager.Call(addrInfo.ID, func(p *p2p.Peer) {
		peerRelation = p.Relation
	})

	switch peerRelation {
	case p2p.PeerRelationKnown:
		clearFromAutopeeringSelector(ev)

	case p2p.PeerRelationUnknown:
		clearFromAutopeeringSelector(ev)

	case p2p.PeerRelationAutopeered:
		handleAlreadyAutopeered(addrInfo)

	default:
		noRelationFunc()
	}
}

// logs a warning about a from the selector seen peer which is already autopeered.
func handleAlreadyAutopeered(addrInfo *libp2p.AddrInfo) {
	Plugin.LogWarnf("peer is already autopeered %s", addrInfo.ID.ShortString())
}

// clears an already statically peered from the autopeering selector.
func clearFromAutopeeringSelector(ev *selection.PeeringEvent) {
	peerID, err := autopeering.HivePeerToPeerID(ev.Peer)
	if err != nil {
		Plugin.LogWarnf("unable to convert selection autopeering peer to peerID: %s", err)
		return
	}

	if deps.AutopeeringManager.Selection() != nil {
		Plugin.LogInfof("peer is statically peered already %s, removing from autopeering selection protocol", peerID.ShortString())
		deps.AutopeeringManager.Selection().RemoveNeighbor(ev.Peer.ID())
	}
}

func attachEvents() {

	if deps.AutopeeringManager.Discovery() != nil {
		deps.AutopeeringManager.Discovery().Events().PeerDiscovered.Attach(onDiscoveryPeerDiscovered)
		deps.AutopeeringManager.Discovery().Events().PeerDeleted.Attach(onDiscoveryPeerDeleted)
	}

	if deps.AutopeeringManager.Selection() != nil {
		// notify the selection when a connection is closed or failed.
		deps.PeeringManager.Events.Disconnected.Attach(onPeerDisconnected)
		deps.PeeringManager.Events.RelationUpdated.Attach(onAutopeerBecameKnown)
		deps.AutopeeringManager.Selection().Events().SaltUpdated.Attach(onSelectionSaltUpdated)
		deps.AutopeeringManager.Selection().Events().OutgoingPeering.Attach(onSelectionOutgoingPeering)
		deps.AutopeeringManager.Selection().Events().IncomingPeering.Attach(onSelectionIncomingPeering)
		deps.AutopeeringManager.Selection().Events().Dropped.Attach(onSelectionDropped)
	}
}

func detachEvents() {

	if deps.AutopeeringManager.Discovery() != nil {
		deps.AutopeeringManager.Discovery().Events().PeerDiscovered.Detach(onDiscoveryPeerDiscovered)
		deps.AutopeeringManager.Discovery().Events().PeerDeleted.Detach(onDiscoveryPeerDeleted)
	}

	if deps.AutopeeringManager.Selection() != nil {
		deps.PeeringManager.Events.Disconnected.Detach(onPeerDisconnected)
		deps.PeeringManager.Events.RelationUpdated.Detach(onAutopeerBecameKnown)
		deps.AutopeeringManager.Selection().Events().SaltUpdated.Detach(onSelectionSaltUpdated)
		deps.AutopeeringManager.Selection().Events().OutgoingPeering.Detach(onSelectionOutgoingPeering)
		deps.AutopeeringManager.Selection().Events().IncomingPeering.Detach(onSelectionIncomingPeering)
		deps.AutopeeringManager.Selection().Events().Dropped.Detach(onSelectionDropped)
	}
}
