// Package stateless implements a PinTracker component for IPFS Cluster, which
// aims to reduce the memory footprint when handling really large cluster
// states.
package stateless

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mtdepin/rep-mgr/api"
	"github.com/mtdepin/rep-mgr/pintracker/optracker"
	"github.com/mtdepin/rep-mgr/state"

	logging "github.com/ipfs/go-log/v2"
	rpc "github.com/libp2p/go-libp2p-gorpc"
	peer "github.com/libp2p/go-libp2p/core/peer"

	"go.opencensus.io/trace"
)

var logger = logging.Logger("pintracker")

const pinsChannelSize = 1024

var (
	// ErrFullQueue is the error used when pin or unpin operation channel is full.
	ErrFullQueue = errors.New("pin/unpin operation queue is full. Try increasing max_pin_queue_size")

	// items with this error should be recovered
	errUnexpectedlyUnpinned = errors.New("the item should be pinned but it is not")
)

// Tracker uses the optracker.OperationTracker to manage
// transitioning shared rep-mgr state (Pins) to the local IPFS node.
type Tracker struct {
	config *Config

	optracker *optracker.OperationTracker

	peerID   peer.ID
	peerName string

	ctx    context.Context
	cancel func()

	getState func(ctx context.Context) (state.ReadOnly, error)

	rpcClient *rpc.Client
	rpcReady  chan struct{}

	priorityPinCh chan *optracker.Operation
	pinCh         chan *optracker.Operation
	unpinCh       chan *optracker.Operation

	shutdownMu sync.Mutex
	shutdown   bool
	wg         sync.WaitGroup
}

// New creates a new StatelessPinTracker.
func New(cfg *Config, pid peer.ID, peerName string, getState func(ctx context.Context) (state.ReadOnly, error)) *Tracker {
	ctx, cancel := context.WithCancel(context.Background())

	spt := &Tracker{
		config:        cfg,
		peerID:        pid,
		peerName:      peerName,
		ctx:           ctx,
		cancel:        cancel,
		getState:      getState,
		optracker:     optracker.NewOperationTracker(ctx, pid, peerName),
		rpcReady:      make(chan struct{}, 1),
		priorityPinCh: make(chan *optracker.Operation, cfg.MaxPinQueueSize),
		pinCh:         make(chan *optracker.Operation, cfg.MaxPinQueueSize),
		unpinCh:       make(chan *optracker.Operation, cfg.MaxPinQueueSize),
	}

	for i := 0; i < spt.config.ConcurrentPins; i++ {
		go spt.opWorker(spt.pin, spt.priorityPinCh, spt.pinCh)
	}
	go spt.opWorker(spt.unpin, spt.unpinCh, nil)

	return spt
}

// we can get our IPFS id from our own monitor ping metrics which
// are refreshed regularly.
func (spt *Tracker) getIPFSID(ctx context.Context) api.IPFSID {
	// Wait until RPC is ready
	<-spt.rpcReady

	var ipfsid api.IPFSID
	err := spt.rpcClient.CallContext(
		ctx,
		"",
		"Cluster",
		"IPFSID",
		peer.ID(""), // local peer
		&ipfsid,
	)
	if err != nil {
		logger.Error(err)
	}
	return ipfsid
}

// receives a pin Function (pin or unpin) and channels.  Used for both pinning
// and unpinning.
func (spt *Tracker) opWorker(pinF func(*optracker.Operation) error, prioCh, normalCh chan *optracker.Operation) {

	var op *optracker.Operation

	for {
		// Process the priority channel first.
		select {
		case op = <-prioCh:
			goto APPLY_OP
		case <-spt.ctx.Done():
			return
		default:
		}

		// Then process things on the other channels.
		// Block if there are no things to process.
		select {
		case op = <-prioCh:
			goto APPLY_OP
		case op = <-normalCh:
			goto APPLY_OP
		case <-spt.ctx.Done():
			return
		}

		// apply operations that came from some channel
	APPLY_OP:
		if clean := applyPinF(pinF, op); clean {
			spt.optracker.Clean(op.Context(), op)
		}
	}
}

// applyPinF returns true if the operation can be considered "DONE".
func applyPinF(pinF func(*optracker.Operation) error, op *optracker.Operation) bool {
	if op.Canceled() {
		// operation was canceled. Move on.
		// This saves some time, but not 100% needed.
		return false
	}
	op.SetPhase(optracker.PhaseInProgress)
	op.IncAttempt()
	err := pinF(op) // call pin/unpin
	if err != nil {
		if op.Type() == optracker.OperationPin && strings.Contains(err.Error(), "pinned") {
			//pinned
			op.SetPhase(optracker.PhaseDone)
			op.Cancel()
			return true // this tells the opWorker to clean the operation from the tracker.
		}

		if op.Canceled() {
			// there was an error because
			// we were canceled. Move on.
			return false
		}
		op.SetError(err)
		op.Cancel()
		return false
	}

	//maitian : change by lyc
	if op.Type() == optracker.OperationPin {
		return false
	}
	op.SetPhase(optracker.PhaseDone)
	op.Cancel()
	return true // this tells the opWorker to clean the operation from the tracker.
}

func (spt *Tracker) pin(op *optracker.Operation) error {
	ctx, span := trace.StartSpan(op.Context(), "tracker/stateless/pin")
	defer span.End()

	logger.Debugf("issuing pin call for %s", op.Cid())
	err := spt.rpcClient.CallContext(
		ctx,
		"",
		"IPFSConnector",
		"Pin",
		op.Pin(),
		&struct{}{},
	)
	if err != nil {
		return err
	}
	return nil
}

func (spt *Tracker) unpin(op *optracker.Operation) error {
	ctx, span := trace.StartSpan(op.Context(), "tracker/stateless/unpin")
	defer span.End()

	logger.Debugf("issuing unpin call for %s", op.Cid())
	err := spt.rpcClient.CallContext(
		ctx,
		"",
		"IPFSConnector",
		"Unpin",
		op.Pin(),
		&struct{}{},
	)
	if err != nil {
		return err
	}
	return nil
}

// Enqueue puts a new operation on the queue, unless ongoing exists.
func (spt *Tracker) enqueue(ctx context.Context, c api.Pin, typ optracker.OperationType) error {
	ctx, span := trace.StartSpan(ctx, "tracker/stateless/enqueue")
	defer span.End()

	//if v, ok := c.Metadata["retry_time"]; ok {
	//	//判断重试次数
	//	int, err := strconv.Atoi(v)
	//	if err != nil {
	//		logger.Error(err)
	//		return err
	//	}
	//}

	logger.Debugf("entering enqueue: pin: %+v", c)
	op := spt.optracker.TrackNewOperation(ctx, c, typ, optracker.PhaseQueued)
	if op == nil {
		return nil // the operation exists and must be queued already.
	}

	var ch chan *optracker.Operation

	switch typ {
	case optracker.OperationPin:
		isPriorityPin := time.Now().Before(c.Timestamp.Add(spt.config.PriorityPinMaxAge)) &&
			op.AttemptCount() <= spt.config.PriorityPinMaxRetries
		op.SetPriorityPin(isPriorityPin)

		if isPriorityPin {
			ch = spt.priorityPinCh
		} else {
			ch = spt.pinCh
		}
	case optracker.OperationUnpin:
		ch = spt.unpinCh
	}

	select {
	case ch <- op:
	default:
		err := ErrFullQueue
		op.SetError(err)
		op.Cancel()
		logger.Error(err.Error())
		return err
	}
	return nil
}

// SetClient makes the StatelessPinTracker ready to perform RPC requests to
// other components.
func (spt *Tracker) SetClient(c *rpc.Client) {
	spt.rpcClient = c
	spt.optracker.SetClient(c)
	close(spt.rpcReady)
}

// Shutdown finishes the services provided by the StatelessPinTracker
// and cancels any active context.
func (spt *Tracker) Shutdown(ctx context.Context) error {
	ctx, span := trace.StartSpan(ctx, "tracker/stateless/Shutdown")
	_ = ctx
	defer span.End()

	spt.shutdownMu.Lock()
	defer spt.shutdownMu.Unlock()

	if spt.shutdown {
		logger.Debug("already shutdown")
		return nil
	}

	logger.Info("stopping StatelessPinTracker")
	spt.cancel()
	spt.wg.Wait()
	spt.shutdown = true
	return nil
}

// Track tells the StatelessPinTracker to start managing a Cid,
// possibly triggering Pin operations on the IPFS daemon.
func (spt *Tracker) Track(ctx context.Context, c api.Pin) error {
	ctx, span := trace.StartSpan(ctx, "tracker/stateless/Track")
	defer span.End()

	logger.Debugf("tracking %s", c.Cid)

	// Sharded pins are never pinned. A sharded pin cannot turn into
	// something else or viceversa like it happens with Remote pins so
	// we just ignore them.
	if c.Type == api.MetaType {
		return nil
	}

	// Trigger unpin whenever something remote is tracked
	// Note, IPFSConn checks with pin/ls before triggering
	// pin/rm.
	if c.IsRemotePin(spt.peerID) {
		op := spt.optracker.TrackNewOperation(ctx, c, optracker.OperationRemote, optracker.PhaseInProgress)
		if op == nil {
			return nil // ongoing unpin
		}
		err := spt.unpin(op)
		op.Cancel()
		if err != nil {
			op.SetError(err)
			return nil
		}

		op.SetPhase(optracker.PhaseDone)
		spt.optracker.Clean(ctx, op)
		return nil
	}

	return spt.enqueue(ctx, c, optracker.OperationPin)
}

// Untrack tells the StatelessPinTracker to stop managing a Cid.
// If the Cid is pinned locally, it will be unpinned.
func (spt *Tracker) Untrack(ctx context.Context, c api.Cid) error {
	ctx, span := trace.StartSpan(ctx, "tracker/stateless/Untrack")
	defer span.End()

	logger.Debugf("untracking %s", c)
	return spt.enqueue(ctx, api.PinCid(c), optracker.OperationUnpin)
}

// StatusAll returns information for all Cids pinned to the local IPFS node.
func (spt *Tracker) StatusAll(ctx context.Context, filter api.TrackerStatus, out chan<- api.PinInfo) error {
	ctx, span := trace.StartSpan(ctx, "tracker/stateless/StatusAll")
	defer span.End()

	ipfsid := spt.getIPFSID(ctx)

	// Any other states are just operation-tracker states, so we just give
	// those and return.
	if !filter.Match(
		api.TrackerStatusPinned | api.TrackerStatusUnexpectedlyUnpinned |
			api.TrackerStatusSharded | api.TrackerStatusRemote) {
		return spt.optracker.GetAllChannel(ctx, filter, ipfsid, out)
	}

	defer close(out)

	// get global state - cluster pinset
	st, err := spt.getState(ctx)
	if err != nil {
		logger.Error(err)
		return err
	}

	//var ipfsRecursivePins map[api.Cid]api.IPFSPinStatus
	var ipfsRecursivePins map[api.Cid]map[peer.ID]api.IPFSPinInfo
	// Only query IPFS if we want to status for pinned items
	if filter.Match(api.TrackerStatusPinned | api.TrackerStatusUnexpectedlyUnpinned) {
		ipfsRecursivePins = make(map[api.Cid]map[peer.ID]api.IPFSPinInfo)
		// At some point we need a full map of what we have and what
		// we don't. The IPFS pinset is the smallest thing we can keep
		// on memory.
		//maitian: change by lyc
		ipfsPinsCh, errCh := spt.ipfsPins(ctx)
		for ipfsPinInfo := range ipfsPinsCh {
			if _, ok := ipfsRecursivePins[ipfsPinInfo.Cid]; !ok {
				ipfsRecursivePins[ipfsPinInfo.Cid] = make(map[peer.ID]api.IPFSPinInfo, 0)
			}
			ipfsRecursivePins[ipfsPinInfo.Cid][ipfsPinInfo.PeerId] = ipfsPinInfo
			//ipfsRecursivePins[ipfsPinInfo.Cid] = ipfsPinInfo.Type
		}
		// If there was an error listing recursive pins then abort.
		err := <-errCh
		if err != nil {
			err := fmt.Errorf("could not get pinset from IPFS: %w", err)
			logger.Error(err)
			return err
		}
	}

	// Prepare pinset streaming
	statePins := make(chan api.Pin, pinsChannelSize)
	go func() {
		err = st.List(ctx, statePins)
		if err != nil {
			logger.Error(err)
		}
	}()

	// a shorthand for this select.
	trySend := func(info api.PinInfo) bool {
		select {
		case <-ctx.Done():
			return false
		case <-spt.ctx.Done():
			return false
		case out <- info:
			return true
		}
	}

	// For every item in the state.
	for p := range statePins {
		select {
		case <-ctx.Done():
		case <-spt.ctx.Done():
		default:
		}

		// if there is an operation, issue that and move on
		//maitian change by lyc
		//info, ok := spt.optracker.GetExists(ctx, p.Cid, ipfsid)
		//if ok && filter.Match(info.Status) {
		//	if !trySend(info) {
		//		return fmt.Errorf("error issuing PinInfo: %w", ctx.Err())
		//	}
		//	continue // next pin
		//}

		// Preliminary PinInfo for this Pin.
		info := api.PinInfo{
			Cid:         p.Cid,
			Name:        p.Name,
			Peer:        spt.peerID,
			Allocations: p.Allocations,
			Origins:     p.Origins,
			Created:     p.Timestamp,
			Metadata:    p.Metadata,

			Pins: []api.PinInfoShort{
				{
					PeerName: spt.peerName,
					//IPFS:     ipfsid.ID,
					//IPFSAddresses: ipfsid.Addresses,
					Status:       api.TrackerStatusUndefined, // TBD
					TS:           p.Timestamp,
					Error:        "",
					AttemptCount: 0,
					PriorityPin:  false,
				},
			},
		}

		//ipfsStatus, pinnedInIpfs := ipfsRecursivePins[p.Cid]
		ipfsStatus, pinnedInIpfs, unPinNodes := checkPins(p.FilterAllocators(spt.peerID), ipfsRecursivePins[p.Cid])

		if ipfsStatus.IsPinned(p.MaxDepth) {
			//get track operation
			op, ok := spt.optracker.GetOperation(ctx, p.Cid)
			if ok {
				//async end, will close operation tracker
				op.SetPhase(optracker.PhaseDone)
				op.Cancel()
				spt.optracker.Clean(op.Context(), op)
			}
		}

		switch {
		case p.Type == api.MetaType:
			info.Status = api.TrackerStatusSharded
		case p.IsRemotePin(spt.peerID):
			info.Status = api.TrackerStatusRemote
		case pinnedInIpfs:
			// No need to filter. pinnedInIpfs is false
			// unless the filter is Pinned |
			// UnexpectedlyUnpinned. We filter at the end.
			//logger.Infof("statusAll cid: %s, status: %s", p.Cid, ipfsStatus)
			info.Status = ipfsStatus.ToTrackerStatus()
		default:
			// Not on an operation
			// Not a meta pin
			// Not a remote pin
			// Not a pin on ipfs

			// We understand that this is something that
			// should be pinned on IPFS and it is not.
			info.Status = api.TrackerStatusUnexpectedlyUnpinned
			info.Error = errUnexpectedlyUnpinned.Error()
			//暂时设置Allocations为unPinNodes， 后面修复流程就只调用unPinNodes里面的节点,可节省网络资源
			info.Allocations = unPinNodes
		}
		if !filter.Match(info.Status) {
			continue
		}

		if !trySend(info) {
			return fmt.Errorf("error issuing PinInfo: %w", ctx.Err())
		}
	}
	return nil
}

// checkPins 检查在所有的allocation 节点都都正确pin了
func checkPins(allocations []string, m map[peer.ID]api.IPFSPinInfo) (status api.IPFSPinStatus, ok bool, unPinNodes []string) {
	if m == nil {
		return status, false, unPinNodes
	}

	if len(allocations) == 0 || len(m) == 0 {
		//is remote
		return api.IPFSPinStatusUnpinned, false, unPinNodes
	}

	status = api.IPFSPinStatusRecursive
	unPinNodes = make([]string, 0)
	for _, alloc := range allocations {
		_, knodeId, err := api.ParseAllocate(alloc)
		if err != nil {
			return status, false, unPinNodes
		}

		if pi, ok := m[knodeId]; !ok || !pi.IsPinned() {
			status = api.IPFSPinStatusUnpinned
			unPinNodes = append(unPinNodes, alloc)
		}
	}

	return status, status == api.IPFSPinStatusRecursive, unPinNodes
}

func (spt *Tracker) GetPinFromState(ctx context.Context, c api.Cid) api.Pin {
	ctx, span := trace.StartSpan(ctx, "tracker/stateless/GetPinFromState")
	defer span.End()

	var gpin api.Pin
	st, err := spt.getState(ctx)
	if err != nil {
		logger.Error(err)
		return gpin
	}

	gpin, err = st.Get(ctx, c)
	if err == state.ErrNotFound {
		return gpin
	}

	return gpin
}

// Status returns information for a Cid pinned to the local IPFS node.
func (spt *Tracker) Status(ctx context.Context, c api.Cid) api.PinInfo {
	ctx, span := trace.StartSpan(ctx, "tracker/stateless/Status")
	defer span.End()

	ipfsid := spt.getIPFSID(ctx)

	// check if c has an inflight operation or errorred operation in optracker
	//if oppi, ok := spt.optracker.GetExists(ctx, c, ipfsid); ok {
	//	return oppi
	//}

	pinInfo := api.PinInfo{
		Cid:  c,
		Peer: spt.peerID,
		Name: "", // etc to be filled later
		Pins: make([]api.PinInfoShort, 0),
	}

	// check global state to see if cluster should even be caring about
	// the provided cid
	var gpin api.Pin
	st, err := spt.getState(ctx)
	if err != nil {
		logger.Error(err)
		addError(&pinInfo, err)
		return pinInfo
	}

	gpin, err = st.Get(ctx, c)
	if err == state.ErrNotFound {
		pinInfo.Status = api.TrackerStatusUnpinned
		return pinInfo
	}
	if err != nil {
		logger.Error(err)
		addError(&pinInfo, err)
		return pinInfo
	}

	// check if c has an inflight operation or errorred operation in optracker
	if oppi, ok := spt.optracker.GetExists(ctx, c, ipfsid); ok {
		//oppi.Allocations = gpin.Allocations
		pinInfo = oppi
		//return oppi
	}

	// The pin IS in the state.
	pinInfo.Name = gpin.Name
	pinInfo.TS = gpin.Timestamp
	pinInfo.Allocations = gpin.Allocations
	pinInfo.Origins = gpin.Origins
	pinInfo.Created = gpin.Timestamp
	pinInfo.Metadata = gpin.Metadata

	// check if pin is a meta pin
	if gpin.Type == api.MetaType {
		pinInfo.Status = api.TrackerStatusSharded
		return pinInfo
	}

	// check if pin is a remote pin
	if gpin.IsRemotePin(spt.peerID) {
		pinInfo.Status = api.TrackerStatusRemote
		return pinInfo
	}

	// else attempt to get status from ipfs node
	//var ips api.IPFSPinStatus
	var ips api.PinDetail
	err = spt.rpcClient.CallContext(
		ctx,
		"",
		"IPFSConnector",
		"PinLsCid",
		gpin,
		&ips,
	)

	for _, ipi := range ips.PinMap {
		ps := api.PinInfoShort{
			//PeerName:      spt.peerName,
			PeerName: ipi.PeerId.String(),
			PeerID:   pinInfo.Peer,
			IPFS:     ipi.PeerId,
			//IPFSAddresses: ipfsid.Addresses,
			Status:       ipi.Type.ToTrackerStatus(),
			TS:           time.Time{},
			Error:        ipi.Error,
			AttemptCount: 0,
			PriorityPin:  false,
		}
		pinInfo.Pins = append(pinInfo.Pins, ps)
	}

	ipfsStatus := ips.IPFSPinStatus.ToTrackerStatus()
	switch ipfsStatus {
	case api.TrackerStatusUnpinned:
		// The item is in the state but not in IPFS:
		// PinError. Should be pinned.
		pinInfo.Status = api.TrackerStatusUnexpectedlyUnpinned
		pinInfo.Error = errUnexpectedlyUnpinned.Error()
	default:
		pinInfo.Status = ipfsStatus
	}

	if err != nil {
		logger.Error(err)
		addError(&pinInfo, err)
		return pinInfo
	}

	return pinInfo
}

// RecoverAll attempts to recover all items tracked by this peer. It returns
// any errors or when it is done re-tracking.
func (spt *Tracker) RecoverAll(ctx context.Context, out chan<- api.PinInfo) error {
	defer close(out)

	ctx, span := trace.StartSpan(ctx, "tracker/stateless/RecoverAll")
	defer span.End()

	statusesCh := make(chan api.PinInfo, 1024)
	go func() {
		err := spt.StatusAll(ctx, api.TrackerStatusUndefined, statusesCh)
		if err != nil {
			logger.Error(err)
		}
	}()

	for st := range statusesCh {
		// Break out if we shutdown. We might be going through
		// a very long list of statuses.\
		logger.Infof("stateless cid: %s status: %s", st.Cid, st.Status)
		select {
		case <-spt.ctx.Done():
			err := fmt.Errorf("RecoverAll aborted: %w", ctx.Err())
			logger.Error(err)
			return err
		default:
			p, err := spt.recoverWithPinInfo(ctx, st)
			if err != nil {
				err = fmt.Errorf("RecoverAll error: %w", err)
				logger.Error(err)
				return err
			}
			if p.Defined() {
				select {
				case <-ctx.Done():
					err = fmt.Errorf("RecoverAll aborted: %w", ctx.Err())
					logger.Error(err)
					return err
				case out <- p:
				}
			}
		}
	}
	return nil
}

// Recover will trigger pinning or unpinning for items in
// PinError or UnpinError states.
func (spt *Tracker) Recover(ctx context.Context, c api.Cid) (api.PinInfo, error) {
	ctx, span := trace.StartSpan(ctx, "tracker/stateless/Recover")
	defer span.End()

	pi := spt.Status(ctx, c)

	recPi, err := spt.recoverWithPinInfo(ctx, pi)
	// if it was not enqueued, no updated pin-info is returned.
	// Use the one we had.
	if !recPi.Defined() {
		recPi = pi
	}
	return recPi, err
}

func (spt *Tracker) recoverWithPinInfo(ctx context.Context, pi api.PinInfo) (api.PinInfo, error) {
	st, err := spt.getState(ctx)
	if err != nil {
		logger.Error(err)
		return api.PinInfo{}, err
	}

	var pin api.Pin

	switch pi.Status {
	case api.TrackerStatusClusterError, api.TrackerStatusPinError, api.TrackerStatusUnexpectedlyUnpinned:
		pin, err = st.Get(ctx, pi.Cid)
		if err != nil { // ignore error - in case pin was removed while recovering
			logger.Warn(err)
			return spt.Status(ctx, pi.Cid), nil
		}
		logger.Infof("Restarting pin operation for %s", pi.Cid)
		pin.Allocations = pi.Allocations

		if spt.isNeedRePin(ctx, pi) {
			err = spt.repin(ctx, pin)
		} else {
			err = spt.enqueue(ctx, pin, optracker.OperationPin)
		}

	case api.TrackerStatusUnpinError:
		logger.Infof("Restarting unpin operation for %s", pi.Cid)
		err = spt.enqueue(ctx, api.PinCid(pi.Cid), optracker.OperationUnpin)
	default:
		// We do not return any information when recover was a no-op
		return api.PinInfo{}, nil
	}
	if err != nil {
		return spt.Status(ctx, pi.Cid), err
	}

	// This status call should be cheap as it would normally come from the
	// optracker and does not need to hit ipfs.
	return spt.Status(ctx, pi.Cid), nil
}

func (spt *Tracker) repin(ctx context.Context, pin api.Pin) error {
	logger.Infof("repinning %s", pin.Cid)
	if spt.rpcClient == nil {
		return errors.New("spt.rpcClient is nil")
	}

	// rePin item
	logger.Infof("==>  repin  %s current allocations : %v ", pin.Cid, pin.Allocations)
	go func() {
		pin.Allocations = nil // force re-allocations
		var pinObj api.Pin
		err := spt.rpcClient.CallContext(
			ctx,
			"",
			"Cluster",
			"Pin",
			pin,
			&pinObj,
		)
		if err != nil {
			logger.Error("repin error: ", err)
		}
		logger.Infof("==> repin  %s new allocations : %v ", pinObj.Cid, pinObj.Allocations)
		logger.Infof("==>  %s repinned", pin.Cid)
	}()
	return nil

}

func (spt *Tracker) isNeedRePin(ctx context.Context, pi api.PinInfo) bool {
	op, ok := spt.optracker.GetOperation(ctx, pi.Cid)
	if !ok {
		return false
	}
	logger.Warnf("==>  operation %s attemptCount %d", pi.Cid, op.AttemptCount())
	//todo： retry times must be defined
	if op.AttemptCount() <= 5 {
		return false
	}

	spt.optracker.Clean(ctx, op)
	op.Cancel()
	return true
}

func (spt *Tracker) ipfsPins(ctx context.Context) (<-chan api.IPFSPinInfo, <-chan error) {
	ctx, span := trace.StartSpan(ctx, "tracker/stateless/ipfspins")
	defer span.End()

	in := make(chan []string, 1) // type filter.
	in <- []string{"recursive", "direct"}
	close(in)
	out := make(chan api.IPFSPinInfo, pinsChannelSize)
	errCh := make(chan error)

	go func() {
		err := spt.rpcClient.Stream(
			ctx,
			"",
			"IPFSConnector",
			"PinLs",
			in,
			out,
		)
		errCh <- err
		close(errCh)
	}()
	return out, errCh
}

// PinQueueSize returns the current size of the pinning queue.
func (spt *Tracker) PinQueueSize(ctx context.Context) (int64, error) {
	return spt.optracker.PinQueueSize(), nil
}

// func (spt *Tracker) getErrorsAll(ctx context.Context) []api.PinInfo {
// 	return spt.optracker.Filter(ctx, optracker.PhaseError)
// }

// OpContext exports the internal optracker's OpContext method.
// For testing purposes only.
func (spt *Tracker) OpContext(ctx context.Context, c api.Cid) context.Context {
	return spt.optracker.OpContext(ctx, c)
}

func addError(pinInfo *api.PinInfo, err error) {
	pinInfo.Error = err.Error()
	pinInfo.Status = api.TrackerStatusClusterError
	if strings.Contains(err.Error(), "not pinned") {
		//has unpinned error
		pinInfo.Status = api.TrackerStatusUnexpectedlyUnpinned
	}
}

func (spt *Tracker) PinCallback(ctx context.Context, cid api.Cid, peerId string, isPinned bool) error {
	ctx, span := trace.StartSpan(ctx, "tracker/stateless/PinCallback")
	defer span.End()
	pinInfo := spt.Status(ctx, cid)

	//get track operation
	op, ok := spt.optracker.GetOperation(ctx, cid)
	if !ok {
		logger.Warnf("PinCallback: not found or has been pinned operation: %s", cid.String())
		return nil
	}

	if !isPinned {
		//knode节点pinned不成功
		err := fmt.Errorf("pin failed by knode: %s, cid: %s ", cid.String())
		op.SetError(err)
		return nil
	}

	allocators := pinInfo.Allocations
	if len(allocators) != len(pinInfo.Pins) {
		logger.Errorf("allocators not match， allocators: %d, pins: %d", len(allocators), len(pinInfo.Pins))
		return nil
	}

	//检查是不是所有的本节点allocator 都pin成功
	success := true
	for _, ps := range pinInfo.Pins {
		if !ps.IsPined() {
			logger.Infof("stateless PinCallback cid: %s , knode pinShort: %s", cid.String(), ps.String())
			success = false
		}
	}

	if !success {
		return nil
	}

	logger.Infof("stateless PinCallback cid: %s  all knode  pin down ", cid.String())
	//async end, will close operation tracker
	op.SetPhase(optracker.PhaseDone)
	op.Cancel()
	spt.optracker.Clean(op.Context(), op)

	return nil
}
