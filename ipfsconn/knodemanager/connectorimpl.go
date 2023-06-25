package knodemanager

import (
	"context"
	"errors"
	"fmt"
	"github.com/mtdepin/rep-mgr/api"
	"github.com/mtdepin/rep-mgr/observations"
	"github.com/mtdepin/rep-mgr/rpcutil"
	gopath "github.com/ipfs/go-path"
	rpc "github.com/libp2p/go-libp2p-gorpc"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.opencensus.io/stats"
	"go.opencensus.io/trace"
	"strings"
	"sync/atomic"
	"time"
)

func (km *KnodeManager) SetClient(client *rpc.Client) {
	km.rmRpcClient = client
	km.rpcReady <- struct{}{}

}

func (km *KnodeManager) Shutdown(ctx context.Context) error {
	//TODO implement me
	km.cancel()
	km.wg.Wait()
	km.shutdown = true
	return nil
}

func (km *KnodeManager) Ready(ctx context.Context) <-chan struct{} {
	return km.ready
}

func (km *KnodeManager) ID(ctx context.Context) (api.IPFSID, error) {
	logger.Debug("=====> KnodeManager ID()")
	ctx, span := trace.StartSpan(ctx, "ipfsconn/KnodeManager/ID")
	defer span.End()

	result := api.IPFSID{
		ID:        km.peerID, //rmId
		Addresses: make([]api.Multiaddr, 0),
		Error:     "",
	}

	//todo: 收集各knode swarm地址信息，应该异步
	nodes := km.getAvalidKnodes()
	if nodes == nil || len(nodes) == 0 {
		return result, nil
	}

	var dests []peer.ID
	for _, node := range nodes {
		dests = append(dests, node.PeerID)
	}

	if len(dests) == 0 {
		return result, nil
	}

	lenDests := len(dests)
	timeout := 5 * time.Second
	ctxs, cancels := rpcutil.CtxsWithTimeout(ctx, lenDests, timeout)
	defer rpcutil.MultiCancel(cancels)

	//make a multi call
	//resps := make([]api.IPFSID, lenDests)
	resps := make([]IpfsIDResp, lenDests)
	logger.Debugf("start MultiCall: KNodeService, GetID. dest: %v", dests)
	errs := km.knodeRpcClient.MultiCall(ctxs, dests, "KNodeService", "GetID", struct{}{}, CopyIpfsIDRespToIfaces(resps))

	if len(errs) > 0 {
		//ignore errors
	}
	for _, res := range resps {
		if res.ID.String() == "12D3KooWFgxSgsBCKpvxccrivpr49iWiAJsetm26uaYBqrynDNip" {
			logger.Info("======> id %s res : %v", res.ID.String(), res.Addresses)

		}
		mAddrs := make([]api.Multiaddr, 0)
		for _, strAddr := range res.Addresses {
			mAddr, err := api.NewMultiaddr(strAddr)
			if err != nil {
				logger.Debug(err)
				continue
			}
			mAddrs = append(mAddrs, mAddr)
		}
		result.Addresses = append(result.Addresses, mAddrs...)
	}

	logger.Debug("knode ID request success: ", result)

	return result, nil

}

func (km *KnodeManager) Pin(ctx context.Context, pin api.Pin) error {
	logger.Info("=====> Pin")
	ctx, span := trace.StartSpan(ctx, "ipfsconn/KnodeManager/Pin")
	defer span.End()

	var multiError = NewMultiError()
	if pin.IsPinEverywhere() {
		//不支持这种类型, replication为-1的情况
		return errors.New("everywhere not support")
	}

	//find peer address on swarm
	originAddress := km.findAddressById(ctx, pin.Origins)
	if len(originAddress) == 0 {
		logger.Warn("no available origin peer address found")
	}

	var dests []peer.ID
	allocators := pin.FilterAllocators(km.peerID)
	for _, allocation := range allocators {
		rmId, knodeId, err := api.ParseAllocate(allocation)
		//logger.Infof("rmid: %s, knodeId: %s", rmId, knodeId)
		if err != nil {
			logger.Error(err)
			multiError.addError(err)
			continue
		}
		knode := km.getPeer(knodeId)
		if knode == nil {
			e := fmt.Errorf("pin error: knode not found, cid: %s, rmID: %s knodeId: %s", pin.Cid.String(), rmId, knodeId)
			multiError.addError(e)
			continue
		}
		//connect to swarm
		if len(originAddress) > 0 {
			go knode.ConnectPeers(ctx, km, originAddress)
		}

		dests = append(dests, knode.PeerID)
	}

	if len(dests) == 0 {
		err := fmt.Errorf("available dests is empty, allocators: %v ", pin.Allocations)
		logger.Error(err)
		return err
	}

	if len(dests) != len(allocators) {
		//有节点异常,记录错误后仍然继续
		err := fmt.Errorf("dests no equals to allocators: dest %d allocators : %d ", len(dests), len(allocators))
		logger.Warn(err)
		multiError.addError(err)
	}

	//todo : always true
	if true {
		//async pin
		return km.PinAsync(ctx, pin, dests, &multiError)
	}

	//pin sync
	//make a multi call
	lenDests := len(dests)
	timeout := km.config.KnodeRequestTimeout
	ctxs, cancels := rpcutil.CtxsWithTimeout(ctx, lenDests, timeout)
	defer rpcutil.MultiCancel(cancels)
	args := PinArgs{
		Operation: "add",
		Cid:       pin.Cid.String(),
		Param: map[string][]string{
			"recursive": {"true"},
		},
	}

	logger.Infof("start MultiCall: KNodeService.Pin, dests: %v,  args: %v", dests, args)
	resps := make([]RawIPFSPinInfo, lenDests)
	errs := km.knodeRpcClient.MultiCall(ctxs, dests, "KNodeService", "Pin", args, CopyRawIPFSPinInfoToIfaces(resps))
	for i, e := range errs {
		if e != nil {
			multiError.addError(e)
			km.handleError(dests[i], e)
		}
	}
	for _, resp := range resps {
		logger.Info("Pin add result: ", resp)
		if resp.Status != CODE_SUCCESS {
			err := fmt.Errorf("pin status: %v", resp.Status)
			logger.Error(err.Error())
			multiError.addError(err)
		}
	}

	//只要一个knode不成功，整体返回异常
	if multiError.error() {
		return multiError.getError()
	}

	return nil
}

func (km *KnodeManager) PinAsync(ctx context.Context, pin api.Pin, dests []peer.ID, multiError *MultiError) error {
	//logger.Info("=====> PinAsync")
	//make a multi call
	lenDests := len(dests)
	ctxs, cancels := rpcutil.CtxsWithTimeout(ctx, lenDests, km.config.KnodeRequestTimeout)
	defer rpcutil.MultiCancel(cancels)
	args := PinArgs{
		Cid: pin.Cid.String(),
		Param: map[string][]string{
			"recursive": {"true"},
		},
	}

	logger.Infof("start MultiCall: KNodeService.PinAsync, dests: %v,  args: %v", dests, args)
	resps := make([]string, lenDests)
	errs := km.knodeRpcClient.MultiCall(ctxs, dests, "KNodeService", "PinAddAsync", args, CopyStringToIfaces(resps))
	logger.Infof(" MultiCall: KNodeService.PinAsync end ，resps： %d ", len(resps))
	for i, e := range errs {
		if e != nil {
			logger.Errorf("KNodeService.PinAsync dest : %s , error: %s", dests[i], e.Error())
			multiError.addError(errs[i])
		}
	}

	if multiError.error() {
		return multiError.getError()
	}

	for i, resp := range resps {
		logger.Infof("Pin add dest: %s, result: %s", dests[i], resp)
		if resp != "pinned" {
			//提前返回
			return nil
		}
	}

	//所有节点都是pinned状态，模拟一个pinned的错误给上层,调用该会判断收到的错误类型
	err := fmt.Errorf("cid: %s, pin status: pinned", pin.Cid.String())
	return err
}

func (km *KnodeManager) Unpin(ctx context.Context, pin api.Pin) error {
	logger.Info("=====> Unpin")

	hash := pin.Cid
	ctx, span := trace.StartSpan(ctx, "ipfsconn/KnodeManager/Unpin")
	defer span.End()

	if km.config.UnpinDisable {
		return errors.New("knode unpinning is disallowed by configuration on this peer")
	}

	allPins, _ := km.PinLsCidFromAllNode(ctx, pin)
	rmAllocators := pin.FilterAllocators(km.peerID)

	if len(allPins) <= len(rmAllocators) {
		//进入到这里，说明已经pin的总数量不大于需要pin的数量，不作unpin处理，但有可能allPins 和节点和rmAllocators节点不对齐
		logger.Warnf("not process unpin")
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, km.config.UnpinTimeout)
	defer cancel()
	var dests []peer.ID

	//除了rmAllocators 外的knode节点，都需要做unpin操作
	for _, node := range allPins {
		needUnpin := true
		for _, allocator := range rmAllocators {
			if strings.Contains(allocator, string(node)) {
				//不需要unpin
				needUnpin = false
				continue
			}
		}
		if needUnpin {
			dests = append(dests, node)
		}
	}

	var multiError = NewMultiError()
	if len(dests) > 0 {
		//make a multi call
		lenDests := len(dests)
		timeout := 10 * time.Second
		ctxs, cancels := rpcutil.CtxsWithTimeout(ctx, lenDests, timeout)
		defer rpcutil.MultiCancel(cancels)

		args := PinArgs{
			Operation: "rm",
			Cid:       pin.Cid.String(),
			Param: map[string][]string{
				"recursive": {"true"},
			},
		}

		resps := make([]RawIPFSPinInfo, lenDests)
		errs := km.knodeRpcClient.MultiCall(ctxs, dests, "KNodeService", "Pin", args, CopyRawIPFSPinInfoToIfaces(resps))

		for _, e := range errs {
			if e != nil {
				multiError.addError(e)
			}
		}
		for _, resp := range resps {
			logger.Info("Pin rm result: ", resp)

			if resp.Status != CODE_SUCCESS {
				err := fmt.Errorf("pin rm status: %s", resp.Status)
				logger.Error(err.Error())
				multiError.addError(err)
			}
		}
	}

	if multiError.error() {
		logger.Error("unpin error: ", multiError.getError())
		//return multiError.getError()
	}

	totalPins := atomic.AddInt64(&km.ipfsPinCount, -1)
	stats.Record(km.ctx, observations.PinsIpfsPins.M(totalPins))

	logger.Info("IPFS Unpin request succeeded:", hash)

	return nil
}

// PinLsCidFromAllNode 获取所有knode节点的pin 状态
func (km *KnodeManager) PinLsCidFromAllNode(ctx context.Context, pin api.Pin) ([]peer.ID, error) {
	var nodes []peer.ID
	var dests []peer.ID

	for _, knode := range km.knodes {
		if !knode.valid() {
			continue
		}
		dests = append(dests, knode.PeerID)
	}

	if len(dests) == 0 {
		logger.Error("PinLsCidFromAllNode: dest is empty, cid: %s", pin.Cid.String())
		return nodes, nil
	}

	//make a multi call
	lenDests := len(dests)
	timeout := 5 * time.Second
	ctxs, cancels := rpcutil.CtxsWithTimeout(ctx, lenDests, timeout)
	defer rpcutil.MultiCancel(cancels)
	args := PinArgs{
		Operation: "ls",
		Cid:       pin.Cid.String(),
		Param: map[string][]string{
			"encoding":        {"json"},
			"stream":          {"false"},
			"stream-channels": {"true"},
			"type":            {"recursive"},
		},
	}
	resps := make([]RawIPFSPinInfo, lenDests)
	_ = km.knodeRpcClient.MultiCall(ctxs, dests, "KNodeService", "Pin", args, CopyRawIPFSPinInfoToIfaces(resps))

	for i, info := range resps {
		logger.Info(info)
		pinInfo := info.transToIPFSInfo()
		if pinInfo.IsPinned() {
			nodes = append(nodes, dests[i])
		}
	}
	return nodes, nil
}

// PinLsCid 只检查本RM节点下多个knode的pin状态
func (km *KnodeManager) PinLsCid(ctx context.Context, pin api.Pin) (api.PinDetail, error) {
	logger.Info("=====> KnodeManager PinLsCid")
	ctx, span := trace.StartSpan(ctx, "ipfsconn/KnodeManager/PinLsCid")
	defer span.End()
	pinDetail := api.PinDetail{
		PinMap: make(map[peer.ID]api.IPFSPinInfo),
	}

	if !pin.Defined() {
		pinDetail.IPFSPinStatus = api.IPFSPinStatusBug
		return pinDetail, errors.New("calling PinLsCid without a defined CID")
	}

	var multiError = NewMultiError()
	if pin.IsPinEverywhere() {
		//不支持这种类型, replication为-1的情况
		pinDetail.IPFSPinStatus = api.IPFSPinStatusError
		return pinDetail, errors.New("everywhere not support")
	}
	var dests []peer.ID
	//过滤本RM节点的allocator
	allocators := pin.FilterAllocators(km.peerID)
	for _, allocation := range allocators {
		rmId, knodeId, err := api.ParseAllocate(allocation)
		if err != nil {
			multiError.addError(err)
			continue
		}
		pi := api.IPFSPinInfo{
			PeerId: knodeId,
			Cid:    pin.Cid,
		}
		knode := km.getPeer(knodeId)
		if knode == nil || !knode.valid() {
			e := fmt.Errorf("PinLsCid error: knode not found, cid: %s, rmID: %s knodeId: %s", pin.Cid.String(), rmId, knodeId)
			multiError.addError(e)
			pi.Error = e.Error()
			pi.Type = api.IPFSPinStatusError
		} else {
			dests = append(dests, knodeId)
		}
		pinDetail.PinMap[knodeId] = pi
	}
	if len(dests) == 0 {
		logger.Errorf("PinLsCid: dest is empty, cid: %s", pin.Cid.String())
		pinDetail.IPFSPinStatus = api.IPFSPinStatusError
		return pinDetail, multiError.getError()
	}

	//make a multi call
	ctx, cancel := context.WithTimeout(ctx, km.config.KnodeRequestTimeout)
	defer cancel()
	args := PinLsArgs{
		Operation: "ls",
		Cid:       pin.Cid.String(),
		Param: map[string][]string{
			"encoding":        {"json"},
			"stream-channels": {"true"},
			"stream":          {"true"},      // true , false
			"type":            {"recursive"}, // recursive , all , direct , indirect
		},
	}
	argChan := make(chan PinLsArgs, 1)
	argChan <- args
	close(argChan)
	originOut := make(chan RawIPFSPinInfo, 20)
	errCh := make(chan []error)

	go func() {
		errs := km.knodeRpcClient.MultiStream(ctx, dests, "KNodeService", "PinStream", argChan, originOut)
		errCh <- errs
	}()

	//handle error
	errs := <-errCh
	for i, e := range errs {
		if e != nil {
			pi := pinDetail.PinMap[dests[i]]
			pi.Error = e.Error()
			pi.Type = api.IPFSPinStatusError
			logger.Errorf("PinLs peerID: %s, error: %s", dests[i], e.Error())
			multiError.addError(e)
			pinDetail.PinMap[dests[i]] = pi
			km.handleError(dests[i], e)
		}
	}

	//handle output
	for info := range originOut {
		select {
		case <-ctx.Done():
			err := fmt.Errorf("aborting pin/ls/cid operation: %w", ctx.Err())
			logger.Error(err)
			pinDetail.IPFSPinStatus = api.IPFSPinStatusError
			return pinDetail, err
		default:
			ipi := info.transToIPFSInfo()
			if !ipi.IsPinned() {
				err := errors.New("ping status: not pinned")
				multiError.addError(err)
				ipi.Error = err.Error()
			}
			pinDetail.PinMap[ipi.PeerId] = ipi
		}
	}

	//只要一个knode的pin状态异常，整体返回异常
	if multiError.error() {
		pinDetail.IPFSPinStatus = api.IPFSPinStatusError
		if strings.Contains(multiError.getError().Error(), "not pinned") {
			//has unpinned error
			pinDetail.IPFSPinStatus = api.IPFSPinStatusUnpinned
		}
		return pinDetail, multiError.getError()
	}

	//success  todo： 暂时只支持recursive
	pinDetail.IPFSPinStatus = api.IPFSPinStatusRecursive
	return pinDetail, nil
}

func (km *KnodeManager) PinLs(ctx context.Context, typeFilters []string, out chan<- api.IPFSPinInfo) error {
	defer close(out)

	ctx, span := trace.StartSpan(ctx, "ipfsconn/KnodeManager/PinLs")
	defer span.End()
	var dests []peer.ID

	//var wg sync.WaitGroup
	for _, knode := range km.knodes {
		if !knode.valid() {
			continue
		}
		dests = append(dests, knode.PeerID)
	}

	if len(dests) > 0 {
		args := PinLsArgs{
			Operation: "ls",
			Param: map[string][]string{
				"encoding":        {"json"},
				"stream-channels": {"true"},
				"stream":          {"true"},      // true , false
				"type":            {"recursive"}, // recursive , all , direct , indirect
			},
		}

		argChan := make(chan PinLsArgs, 1)
		argChan <- args
		close(argChan)
		originOut := make(chan RawIPFSPinInfo, 20)

		go func() {
			errs := km.knodeRpcClient.MultiStream(ctx, dests, "KNodeService", "PinStream", argChan, originOut)
			for i, e := range errs {
				if e != nil {
					logger.Errorf("PinLs peerID: %s, error: %s", dests[i], e.Error())
					km.handleError(dests[i], e)
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				err := fmt.Errorf("aborting pin/ls operation: %w", ctx.Err())
				logger.Error(err)
				return err
			case info, ok := <-originOut:
				if !ok {
					//err := fmt.Errorf(" pin/ls not ok ok ok ok ok ok ok: %w", ctx.Err())
					//logger.Error(err)
					return nil
				}
				logger.Info(info)
				pinInfo := info.transToIPFSInfo()

				//try to write pinInfo to out chan
				select {
				case <-ctx.Done():
					err := fmt.Errorf("aborting pin/ls operation: %w", ctx.Err())
					logger.Error(err)
					return err
				case out <- pinInfo:
				}
			}
		}
	}

	return nil
}

func (km *KnodeManager) ConnectSwarms(ctx context.Context) error {
	logger.Info("===> kmanager ConnectSwarms")
	ctx, span := trace.StartSpan(ctx, "ipfsconn/KnodeManager/ConnectSwarms")
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, km.config.KnodeRequestTimeout)
	defer cancel()

	in := make(chan struct{})
	close(in)
	out := make(chan api.ID)
	go func() {
		err := km.rmRpcClient.Stream(
			ctx,
			"",
			"Cluster",
			"Peers",
			in,
			out,
		)
		if err != nil {
			logger.Error(err)
		}
	}()

	nodes := km.getAvalidKnodes()
	dests := []peer.ID{}
	for _, n := range nodes {
		dests = append(dests, n.PeerID)
	}

	for id := range out {
		ipfsID := id.IPFS
		if id.Error != "" || ipfsID.Error != "" {
			continue
		}

		addrs := []string{}
		addresses := publicIPFSAddresses(id.IPFS.Addresses)
		for _, addr := range addresses {
			if !strings.Contains(addr.String(), "p2p-circuit") {
				logger.Infof("===========>> addr : %s", addr.String())
			}

			addrs = append(addrs, addr.String())
		}

		//add some temp addrs{
		//addrs = append(addrs, "/ip4/10.80.51.56/tcp/4001/p2p/12D3KooWMhSVYdv5PBBVM5cgSseMAUbdSamJV2upZQ35gutwogcX")
		//addrs = append(addrs, "/ip4/10.80.51.55/tcp/4001/p2p/12D3KooWRXPDt9h5rYX9hRpmJwZ2RbTguMicc7srBwZ8uKTwcWc1")
		//addrs = append(addrs, "/ip4/10.80.51.53/tcp/4001/p2p/12D3KooWNz3gcjwirdUhteGbrAi92KKQEfAwPLmmQTyim1ZAShSq")

		logger.Infof("start connect swarm, peerId: %v", dests)
		lenDests := len(dests)
		timeout := 30 * time.Second
		ctxs, cancels := rpcutil.CtxsWithTimeout(ctx, lenDests, timeout)
		defer rpcutil.MultiCancel(cancels)

		var resps = make([]CommonResp, lenDests)
		errs := km.knodeRpcClient.MultiCall(ctxs, dests, "KNodeService", "SwarmConnect", addrs, CopyCommonRespToIfaces(resps))
		if len(errs) > 0 {
			for i, e := range errs {
				if e != nil {
					logger.Debugf("kmanager connect swarm: %s, err: %s", dests[i], e)
				}
			}
		}
		for _, resp := range resps {
			logger.Debug("kmanager swarmConnect resp: ", resp)
		}
		logger.Debugf("kmanager complete connected to %v", addrs)
	}
	return nil
}

func (km *KnodeManager) SwarmPeers(ctx context.Context) ([]peer.ID, error) {
	ctx, span := trace.StartSpan(ctx, "ipfsconn/KnodeManager/SwarmPeers")
	defer span.End()

	err := fmt.Errorf("error: unImplement SwarmPeers")
	logger.Error(err)
	return nil, err
}

func (km *KnodeManager) ConfigKey(keypath string) (interface{}, error) {
	err := fmt.Errorf("error: unImplement ConfigKey")
	logger.Error(err)
	return nil, err
}

func (km *KnodeManager) RepoStat(ctx context.Context) (api.IPFSRepoStat, error) {
	ctx, span := trace.StartSpan(ctx, "ipfsconn/KnodeManager/RepoStat")
	defer span.End()

	//TODO implement me
	return api.IPFSRepoStat{
		RepoSize:   0,
		StorageMax: 0,
	}, nil
}

func (km *KnodeManager) RepoGC(ctx context.Context) (api.RepoGC, error) {
	ctx, span := trace.StartSpan(ctx, "ipfsconn/KnodeManager/RepoGC")
	defer span.End()

	var dests []peer.ID

	for _, knode := range km.knodes {
		if !knode.valid() {
			continue
		}

		dests = append(dests, knode.PeerID)
	}

	if len(dests) > 0 {
		lenDests := len(dests)
		timeout := 5 * time.Second
		ctxs, cancels := rpcutil.CtxsWithTimeout(ctx, lenDests, timeout)
		defer rpcutil.MultiCancel(cancels)

		var resps = make([]CommonResp, lenDests)
		errs := km.knodeRpcClient.MultiCall(ctxs, dests, "KNodeService", "RepoGC", struct{}{}, CopyCommonRespToIfaces(resps))
		if len(errs) > 0 {
			//ignore errors
		}
		for _, resp := range resps {
			logger.Info("RepoGC response: ", resp)
		}

		logger.Info(" RepoGC request succeeded:", dests)
	}

	return api.RepoGC{}, nil
}

func (km *KnodeManager) Resolve(ctx context.Context, path string) (api.Cid, error) {
	ctx, span := trace.StartSpan(ctx, "ipfsconn/KnodeManager/Resolve")
	defer span.End()

	validPath, err := gopath.ParsePath(path)
	if err != nil {
		logger.Error("could not parse path: " + err.Error())
		return api.CidUndef, err
	}
	//if !strings.HasPrefix(path, "/ipns") && validPath.IsJustAKey() {
	ci, _, err := gopath.SplitAbsPath(validPath)
	return api.NewCid(ci), err
	//}

	//ci, _, err := gopath.SplitAbsPath(gopath.FromString(resp.Path))
	//return api.NewCid(ci), err
}

func (km *KnodeManager) BlockStream(ctx context.Context, metas <-chan api.NodeWithMeta) error {
	ctx, span := trace.StartSpan(ctx, "ipfsconn/KnodeManager/BlockStream")
	defer span.End()

	err := fmt.Errorf("BlockStream: unexpect here")
	logger.Error(err)
	return err
}

func (km *KnodeManager) BlockGet(ctx context.Context, cid api.Cid) ([]byte, error) {
	ctx, span := trace.StartSpan(ctx, "ipfsconn/KnodeManager/BlockGet")
	defer span.End()

	err := fmt.Errorf("BlockGet: unexpect here")
	logger.Error(err)
	return nil, err
}

func (km *KnodeManager) pinCallback(ctx context.Context, pin RawIPFSPinInfo, isPinned bool) error {
	ctx, span := trace.StartSpan(ctx, "ipfsconn/KnodeManager/pinCallback")
	defer span.End()
	c, err := api.DecodeCid(pin.Cid)
	if err != nil {
		logger.Error(err)
		return err
	}

	err = km.stateTracker.PinCallback(ctx, c, pin.PeerId, isPinned)
	if err != nil {
		logger.Error(err)
		return err
	}

	return nil
}
