package knodemanager

import (
	"context"
	"fmt"
	"github.com/mtdepin/rep-mgr/api"
	"github.com/mtdepin/rep-mgr/config"
	"github.com/mtdepin/rep-mgr/controllerclient"
	"github.com/mtdepin/rep-mgr/observations"
	"github.com/mtdepin/rep-mgr/pintracker/stateless"
	"github.com/mtdepin/rep-mgr/rpcutil"
	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p"
	gorpc "github.com/libp2p/go-libp2p-gorpc"
	rpc "github.com/libp2p/go-libp2p-gorpc"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/multiformats/go-multiaddr"
	"go.opencensus.io/stats"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var logger = logging.Logger("knodemanager")

const listenAddr = "/ip4/0.0.0.0/udp/9500/quic"

var protocolID = protocol.ID("/p2p/rpc/rm")

// 检查knode健康间隔
var CHECK_KNODE_INTERVAL = 60 * time.Second
var CHECK_KNODE_DISCARD = 300 * time.Second

const DefaultMetricTTL = 30 * time.Second

type RMMetric struct {
	Name          string
	Peer          peer.ID           //RM节点id
	Group         string            //该RM节点所属分组
	Tag           string            //标签，与分组一样，用于节点分配
	Value         string            //RM管理的所有knode总可用空间
	Expire        int64             //本条信息过期时间
	Valid         bool              //是否可用，既该节点是否可分配
	Weight        int64             //权重
	Partitionable bool              //是否可分区（暂不明白用法）
	ReceivedAt    int64             //时间
	KnodeList     []api.KnodeMetric //  该RM管理下的knode列表）
}
type KnodeManager struct {
	// struct alignment! These fields must be up-front.
	mux               sync.RWMutex
	updateMetricCount uint64
	ipfsPinCount      int64

	config Config

	metric RMMetric

	peerID peer.ID
	host   host.Host

	ctx    context.Context
	cancel func()
	ready  chan struct{}

	nodeAddr string

	rmRpcClient *rpc.Client
	rpcReady    chan struct{}

	knodes map[peer.ID]*Knode

	failedRequests atomic.Uint64 // count failed requests.
	reqRateLimitCh chan struct{}

	shutdownLock sync.Mutex
	shutdown     bool
	wg           sync.WaitGroup

	knodeRpcClient *rpc.Client

	controllerClient *controllerclient.ControllerConn

	stateTracker *stateless.Tracker

	peerChacker *KnodeChecker
}

type NodeStatus int

const (
	CONNECTED  NodeStatus = iota //已连接
	REGISTED                     //已注册
	Valid                        //健康可用
	Invalid                      //心跳延迟
	DISCONNECT                   //连接断开
	DISCARDED                    //被丢弃的
)

func NewKnodeManager(identity *config.Identity, peerID peer.ID, controllerClient *controllerclient.ControllerConn) *KnodeManager {
	ctx, cancel := context.WithCancel(context.Background())

	manager := &KnodeManager{
		config:           DefaultConfig(),
		peerID:           peerID,
		host:             nil,
		ctx:              ctx,
		cancel:           cancel,
		ready:            make(chan struct{}),
		rpcReady:         make(chan struct{}, 1),
		reqRateLimitCh:   make(chan struct{}),
		knodes:           make(map[peer.ID]*Knode),
		controllerClient: controllerClient,
	}

	manager.startRpc(identity)
	manager.knodeRpcClient = gorpc.NewClient(manager.host, protocolID)

	manager.peerChacker = NewKnodeChecker(manager.ctx, manager)

	go manager.run()

	return manager
}

func (km *KnodeManager) run() {
	<-km.rpcReady

	//check manager is ready
	km.waitForReady()

	//check knode is valid  regularly
	go km.startCheckKnode()

	// Do not shutdown while launching threads
	// -- prevents race conditions with ipfs.wg.
	km.shutdownLock.Lock()
	defer km.shutdownLock.Unlock()

	if km.config.ConnectSwarmsDelay == 0 {
		return
	}

	// This runs ipfs swarm connect to the daemons of other cluster members
	km.wg.Add(1)
	go func() {
		defer km.wg.Done()

		// It does not hurt to wait a little bit. i.e. think cluster
		// peers which are started at the same time as the ipfs
		// daemon...
		tmr := time.NewTimer(km.config.ConnectSwarmsDelay)
		defer tmr.Stop()
		select {
		case <-tmr.C:
			// do not hang this goroutine if this call hangs
			// otherwise we hang during shutdown
			go km.ConnectSwarms(km.ctx)
		case <-km.ctx.Done():
			return
		}
	}()

	//start peer chack
	km.peerChacker.run()

}

func (km *KnodeManager) Alerts() <-chan api.Alert {
	return km.peerChacker.alertCh
}

// waitForReady 等待至少有一个knode节点连上来
func (km *KnodeManager) waitForReady() {
	i := 0
	for {
		select {
		case <-km.ctx.Done():
			return
		default:
		}
		i++
		if len(km.getAvalidKnodes()) > 0 {
			close(km.ready)
			break
		}
		if i%60 == 0 {
			logger.Warningf("no knode connect , waiting for at least 1 knode connected,    %d retries", i)
		}

		// Requests will be rate-limited when going faster.
		time.Sleep(time.Second)
	}
}

func (km *KnodeManager) SetStateTracker(tracker *stateless.Tracker) {
	km.stateTracker = tracker
}

func (km *KnodeManager) GetMetric() []api.Metric {
	logger.Debugf("===> knodeManager GetMetric")
	km.mux.RLock()
	km.mux.RUnlock()
	t := time.Now()
	t = t.Add(time.Duration(30) * time.Second)

	var validSpace int64 = 0

	list := make([]api.KnodeMetric, len(km.knodes))
	count := 0
	for _, node := range km.knodes {
		if node.status == Valid {
			validSpace = validSpace + node.metrics.Value
			list[count] = node.metrics
			count++
		}
	}

	list = list[:count]

	metric := api.Metric{
		Name:          "freespace",
		Peer:          km.peerID,
		Group:         "sichuan",
		Tag:           "dianxin",
		Value:         fmt.Sprintf("%d", validSpace),
		Expire:        t.UnixNano(),
		Valid:         true,
		Weight:        validSpace,
		Partitionable: false,
		KnodeList:     list,
	}
	if len(list) == 0 {
		logger.Error("no knode")
		metric.Valid = false
	}

	metric.SetTTL(DefaultMetricTTL)

	stats.Record(context.Background(), observations.InformerDisk.M(metric.Weight))

	print(metric)

	dicardedMetric := km.GetDiscardedMetric()
	if len(dicardedMetric.KnodeList) > 0 {
		return []api.Metric{metric, dicardedMetric}
	}

	return []api.Metric{metric}

}

func (km *KnodeManager) GetDiscardedMetric() api.Metric {
	list := make([]api.KnodeMetric, len(km.knodes))
	count := 0
	for _, node := range km.knodes {
		if node.status == DISCARDED {
			list[count] = node.metrics
			count++
		}
	}

	list = list[:count]

	metric := api.Metric{
		Name:          "discarded",
		Peer:          km.peerID,
		Group:         "sichuan",
		Tag:           "dianxin",
		Valid:         true,
		Partitionable: false,
		KnodeList:     list,
	}

	metric.SetTTL(DefaultMetricTTL)

	return metric
}

func (km *KnodeManager) addPeer(node Knode) {
	km.mux.Lock()
	defer km.mux.Unlock()

	knode, ok := km.knodes[node.PeerID]
	if !ok {
		km.knodes[node.PeerID] = &node
		return
	}
	//update node info
	knode.status = node.status
	knode.timestamp = node.timestamp
	km.knodes[node.PeerID] = knode

}

func (km *KnodeManager) getPeer(id peer.ID) *Knode {
	km.mux.RLock()
	defer km.mux.RUnlock()

	node, ok := km.knodes[id]
	if ok {
		return node
	}
	return nil

}

func (km *KnodeManager) removePeer(id peer.ID) {
	km.mux.Lock()
	defer km.mux.Unlock()

	_, ok := km.knodes[id]
	if ok {
		delete(km.knodes, id)
		return
	}
}

func (km *KnodeManager) updatePeer(newNode Knode) {
	km.mux.Lock()
	defer km.mux.Unlock()
	km.knodes[newNode.PeerID] = &newNode

	//update alert
	if newNode.valid() {
		km.peerChacker.ResetAlerts(newNode.PeerID)
	}
}

func (km *KnodeManager) updatePeerStatus(id peer.ID, newStatus NodeStatus) {
	km.mux.Lock()
	defer km.mux.Unlock()

	node, ok := km.knodes[id]
	if ok {
		node.status = newStatus
		node.timestamp = time.Now()
		if newStatus == DISCONNECT || newStatus == Invalid {
			node.metrics.Valid = false
		}
		return
	}
	logger.Warnf("knode not found: %s ", id.String())
}

func (km *KnodeManager) startRpc(cfg *config.Identity) {
	host := km.createPeer(cfg, listenAddr)
	logger.Infof("KnodeManager hosts ID is %s\n", host.ID().Pretty())
	for _, addr := range host.Addrs() {
		ipfsAddr, err := multiaddr.NewMultiaddr("/ipfs/" + host.ID().Pretty())
		if err != nil {
			panic(err)
		}
		peerAddr := addr.Encapsulate(ipfsAddr)
		logger.Infof("KnodeManager listening on %s\n", peerAddr)
	}

	//regis server side rpc
	rpcHost := gorpc.NewServer(host, protocolID)
	svc := RMService{
		manager:          km,
		controllerClient: km.controllerClient,
	}
	err := rpcHost.Register(&svc)
	if err != nil {
		panic(err)
	}

	km.host = host

	km.watchConnection()

	logger.Infof("knode manager: startRpc done")
}

func (km *KnodeManager) createPeer(cfg *config.Identity, listenAddr string) host.Host {
	// Create a new libp2p host
	cmg, _ := connmgr.NewConnManager(1000, 2000)
	h, err := libp2p.New(
		libp2p.Identity(cfg.PrivateKey),
		libp2p.ListenAddrStrings(listenAddr),
		libp2p.ConnectionManager(cmg),
		//libp2p.Muxer()
		//libp2p.SetDefaultServiceLimits()
		//libp2p.EnableRelay(),
	)

	if err != nil {
		panic(err)
	}

	//_, err = relay.New(h)
	//if err != nil {
	//	logger.Errorf("Failed to instantiate the relay: %v", err)
	//	panic(err)
	//}

	return h
}

func (km *KnodeManager) watchConnection() {
	km.host.Network().Notify(
		&network.NotifyBundle{
			ConnectedF: func(nw network.Network, conn network.Conn) {
				logger.Info("new connections !!")
				remoteAddr := conn.RemoteMultiaddr().String()
				logger.Info("remote address:  ", remoteAddr)
				logger.Info("remote peerId:  ", conn.RemotePeer().String())

				node := Knode{
					PeerID:    conn.RemotePeer(),
					timestamp: time.Now(),
					status:    CONNECTED,
				}
				km.addPeer(node)

				go func() {
					time.Sleep(10 * time.Second)
					km.ConnectSwarms(km.ctx)
				}()
			},
			DisconnectedF: func(nw network.Network, conn network.Conn) {
				logger.Warn("disconnected !!")
				remoteAddr := conn.RemoteMultiaddr().String()
				logger.Warn("remote address:  ", remoteAddr)
				logger.Warn("remote peerId:  ", conn.RemotePeer().String())
				km.updatePeerStatus(conn.RemotePeer(), DISCONNECT)
			},
			ListenCloseF: func(n network.Network, m multiaddr.Multiaddr) {
				logger.Warnf("ListenClose: %s", m.String())
			},
		},
	)
}

func (km *KnodeManager) startCheckKnode() {
	logger.Infof("==> start check knode status")
	defer func() {
		logger.Warn("check node status process end!")
	}()
	for {
		if len(km.knodes) == 0 {
			logger.Warn("no kepler-node connected!")
		}

		for pid, knode := range km.knodes {
			now := time.Now()
			if knode.status == Valid && now.After(knode.timestamp.Add(CHECK_KNODE_INTERVAL)) {
				logger.Warnf("node metric delay: %s", knode.PeerID)
				//状态标记为不可用
				km.updatePeerStatus(pid, Invalid)
			} else if now.After(knode.timestamp.Add(CHECK_KNODE_DISCARD)) {
				logger.Warnf("knode %s is discarded", knode.PeerID.String())
				//状态标记为待丢弃
				km.updatePeerStatus(pid, DISCARDED)
			}
		}

		time.Sleep(CHECK_KNODE_INTERVAL)
	}
}

func (km *KnodeManager) getAvalidKnodes() []*Knode {
	var nodes []*Knode
	for _, knode := range km.knodes {
		if knode.status == Invalid || knode.status == DISCONNECT {
			continue
		}
		if !knode.metrics.Valid {
			continue
		}
		nodes = append(nodes, knode)
	}
	return nodes
}

func (km *KnodeManager) findAddressById(ctx context.Context, mulAddrs []api.Multiaddr) []string {
	if len(mulAddrs) == 0 {
		return nil
	}
	nodes := km.getAvalidKnodes()
	result := make([]string, 0)
	if nodes == nil || len(nodes) == 0 {
		return nil
	}

	var dests []peer.ID
	for _, node := range nodes {
		dests = append(dests, node.PeerID)
	}

	peers := make([]string, len(mulAddrs))
	for i, ma := range mulAddrs {
		//id, err := ma.ValueForProtocol(multiaddr.P_P2P)
		//if err != nil {
		//	logger.Error(err)
		//	continue
		//}
		//peers[i] = id

		arr := strings.Split(ma.String(), "/")

		if len(arr) > 0 {
			peers[i] = arr[len(arr)-1]
		}

	}

	if len(dests) == 0 || len(peers) == 0 {
		return nil
	}

	lenDests := len(dests)
	timeout := 5 * time.Second
	ctxs, cancels := rpcutil.CtxsWithTimeout(ctx, lenDests, timeout)
	defer rpcutil.MultiCancel(cancels)

	//make a multi call
	resps := make([][]string, lenDests)
	errs := km.knodeRpcClient.MultiCall(ctxs, dests, "KNodeService", "FindPeer", peers, CopyStringSliceToIfaces(resps))

	for i, e := range errs {
		if e != nil {
			km.handleError(dests[i], e)
		}
	}
	for _, res := range resps {
		result = append(result, res...)
	}

	logger.Infof("knode findAddressById request complete, peerId: %v,  dests: %v, errs: %v ", mulAddrs, dests, errs)

	return result

}

func (km *KnodeManager) handleError(id peer.ID, e error) {
	if strings.Contains(e.Error(), "stream reset") {
		logger.Warn("close peer: ", id)
		km.host.Network().ClosePeer(id)
	}
}
