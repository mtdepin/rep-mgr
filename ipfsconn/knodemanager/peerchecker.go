package knodemanager

import (
	"context"
	"errors"
	"github.com/mtdepin/rep-mgr/api"
	"github.com/libp2p/go-libp2p/core/peer"
	"sync"
	"time"
)

var PeerMaxAlertThreshold = 1

var ErrAlertChannelFull = errors.New("alert channel is full")

type KnodeChecker struct {
	ctx           context.Context
	alertCh       chan api.Alert
	km            *KnodeManager
	failedPeersMu sync.Mutex
	failedPeers   map[peer.ID]int
}

func NewKnodeChecker(ctx context.Context, manager *KnodeManager) *KnodeChecker {
	return &KnodeChecker{
		ctx:         ctx,
		alertCh:     make(chan api.Alert, 2),
		km:          manager,
		failedPeers: make(map[peer.ID]int),
	}
}

func (kc *KnodeChecker) run() {
	go kc.Watch(kc.ctx, 5*time.Second)
}

func (kc *KnodeChecker) Watch(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	for {
		select {
		case <-ticker.C:
			kc.CheckAll()
		case <-ctx.Done():
			ticker.Stop()
			return
		}
	}
}

func (kc *KnodeChecker) Alerts() <-chan api.Alert {
	return kc.alertCh
}

func (kc *KnodeChecker) CheckAll() {
	for _, node := range kc.km.knodes {
		if node.status == DISCARDED {
			kc.alert(kc.km.peerID, node)
		}
	}

}

func (kc *KnodeChecker) ResetAlerts(pid peer.ID) {
	kc.failedPeersMu.Lock()
	defer kc.failedPeersMu.Unlock()

	_, ok := kc.failedPeers[pid]
	if !ok {
		return
	}
	delete(kc.failedPeers, pid)
}

func (kc *KnodeChecker) alert(pid peer.ID, node *Knode) error {
	logger.Info("start alert: ", node.PeerID)
	kc.failedPeersMu.Lock()
	defer kc.failedPeersMu.Unlock()

	if _, ok := kc.failedPeers[pid]; !ok {
		kc.failedPeers[pid] = 0
	}

	kc.failedPeers[pid]++
	failCount := kc.failedPeers[pid]

	logger.Infof("alert peer: %s ,  current count: %d, threshold: %d ", node.PeerID, failCount, PeerMaxAlertThreshold)

	// If above threshold, do not send alert
	if failCount > PeerMaxAlertThreshold {
		// Cleanup old metrics eventually
		if failCount >= 50 {
			delete(kc.failedPeers, pid)
			//remove this peer from connection manager
			kc.km.removePeer(node.PeerID)
		}
		return nil
	}

	lastMetric := api.Metric{
		Name: node.metrics.Name,
		Peer: pid,
	}

	alrt := api.Alert{
		Metric:      lastMetric,
		TriggeredAt: time.Now(),
	}
	logger.Infof("send alert, peer: %s, content: %v", node.PeerID, alrt)
	select {
	case kc.alertCh <- alrt:
	default:
		return ErrAlertChannelFull
	}
	return nil
}
