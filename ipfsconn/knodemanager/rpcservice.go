package knodemanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mtdepin/rep-mgr/api"
	"github.com/mtdepin/rep-mgr/controllerclient"
	gorpc "github.com/libp2p/go-libp2p-gorpc"
	"go.opencensus.io/trace"
	"strings"
	"time"
)

type ErrorCode int

const (
	CODE_SUCCESS ErrorCode = 1
	CODE_FAILD   ErrorCode = 0
	CODE_EXPIRE  ErrorCode = 301

	CODE_INVALID_PARAMETER     ErrorCode = 501
	CODE_INVALID_INTERNALERROR ErrorCode = 502
)

type RMService struct {
	manager          *KnodeManager
	controllerClient *controllerclient.ControllerConn
}

type RegisArgs struct {
	PeerID  string
	Version string
	Ip      string
	Port    string
}

type UploadFileArgs struct {
	PeerID  string //节点id
	OrderID string //controller上传订单号
	Cid     string // cid
	Name    string //file name
	Fid     string //文件id
	Region  string
	Origins string
}

type CommonResp struct {
	Status ErrorCode
}

type PinAsyncResp struct {
	pinStatus string // pinning, pinned
}

func CopyStringSliceToIfaces(in [][]string) []interface{} {
	ifaces := make([]interface{}, len(in))
	for i := range in {
		in[i] = []string{}
		ifaces[i] = &(in[i])
	}
	return ifaces
}

func CopyStringToIfaces(in []string) []interface{} {
	ifaces := make([]interface{}, len(in))
	for i := range in {
		in[i] = ""
		ifaces[i] = &(in[i])
	}
	return ifaces
}

func CopyIpfsIDRespToIfaces(in []IpfsIDResp) []interface{} {
	ifaces := make([]interface{}, len(in))
	for i := range in {
		in[i] = IpfsIDResp{}
		ifaces[i] = &(in[i])
	}
	return ifaces
}

func CopyCommonRespToIfaces(in []CommonResp) []interface{} {
	ifaces := make([]interface{}, len(in))
	for i := range in {
		in[i] = CommonResp{}
		ifaces[i] = &(in[i])
	}
	return ifaces
}

func (svc *RMService) Regist(ctx context.Context, args RegisArgs, resp *CommonResp) error {
	logger.Info("==> Regist")
	remotepeerID, _ := gorpc.GetRequestSender(ctx)
	logger.Info("remotepeer: ", remotepeerID)
	print(args)

	if remotepeerID.String() != args.PeerID {
		resp.Status = CODE_INVALID_PARAMETER
		err := fmt.Errorf("peerId not match: %s, %s", remotepeerID, args.PeerID)
		return err
	}

	node := Knode{
		PeerID:    remotepeerID,
		Version:   args.Version,
		Ip:        args.Ip,
		Port:      args.Port,
		timestamp: time.Now(),
		status:    REGISTED,
	}

	svc.manager.addPeer(node)
	resp.Status = CODE_SUCCESS
	return nil
}

func (svc *RMService) PushMetric(ctx context.Context, m api.KnodeMetric, resp *CommonResp) error {
	logger.Debug("---> PushMetric")
	logger.Debugf("metric: %v", m)
	if err := svc.checkMetricValid(m); err != nil {
		logger.Error(err)
		resp.Status = CODE_INVALID_PARAMETER
		return err
	}

	node := svc.manager.getPeer(m.PeerID)
	if node == nil {
		err := fmt.Errorf("knode not found: %s ", m.PeerID)
		logger.Error(err)
		resp.Status = CODE_INVALID_INTERNALERROR
		return err
	}

	//check expire
	if time.Now().UTC().After(m.Expire.Local()) {
		err := fmt.Errorf("ignore this metric, because it is expired: %v, now: %v,  pid: %s", m.Expire.Local(), time.Now(), m.PeerID)
		logger.Error(err)
		resp.Status = CODE_EXPIRE
		return err
	}
	//valid, update node
	node.status = Valid
	node.timestamp = time.Now()
	m.Valid = true
	node.metrics = m

	svc.manager.updatePeer(*node)

	//success
	resp.Status = CODE_SUCCESS
	return nil
}

func (svc *RMService) PinCallback(ctx context.Context, pin RawIPFSPinInfo, resp *CommonResp) error {
	ctx, span := trace.StartSpan(ctx, "ipfsconn/knode/PinCallback")
	defer span.End()
	logger.Infof("---> knode PinCallback: args: %v ", pin)
	isPinned := pin.Status == CODE_SUCCESS

	return svc.manager.pinCallback(ctx, pin, isPinned)
}

func (svc *RMService) checkMetricValid(m api.KnodeMetric) error {
	if m.PeerID == "" {
		return errors.New("peerID is empty")
	}
	if m.Address == "" {
		return errors.New("peer address is empty")
	}

	now := time.Now()
	if now.After(m.Expire) {
		logger.Warnf("knode metric is expire: expire: %s , now: %s", m.Expire.String(), now.String())
	}

	if m.Value < 0 {
		return errors.New("peer value is invalid")
	}

	return nil

}

func (svc *RMService) UploadFile(ctx context.Context, args UploadFileArgs, replyType *CommonResp) error {
	logger.Info("---> UploadFile")
	logger.Info("args: ", args)
	logger.Info("peerID: ", args.PeerID)

	//post a request to controller

	var param = struct {
		OrderId string `json:"order_id"`
		Fid     string `json:"fid"`
		Cid     string `json:"cid"`
		Status  int    `json:"status"`
		Region  string `json:"region"`
		Origins string `json:"origins"`
	}{
		OrderId: args.OrderID,
		Cid:     args.Cid,
		Fid:     args.Fid,
		Region:  args.Region,
		//todo:此参数无意义
		Status:  1,
		Origins: args.Origins,
	}

	jsonBytes, _ := json.Marshal(param)
	body := strings.NewReader(string(jsonBytes))
	url := "task_tracker/v1/callbackUpload"
	logger.Infof("PostCtx: url: %s, body: %s", url, string(jsonBytes))
	result, err := svc.controllerClient.PostCtx(
		ctx,
		url,
		"application/json",
		body,
	)
	if err != nil {
		logger.Error(err)
		replyType.Status = CODE_INVALID_INTERNALERROR
		return err
	}

	//parse result
	var cResp CommonResp
	err = json.Unmarshal(result, &cResp)
	if err != nil {
		logger.Error(err)
		replyType.Status = CODE_INVALID_INTERNALERROR
		return err
	}

	logger.Infof("UploadFile success : %v ", args)
	//set reply
	*replyType = cResp
	//replyType.Status = CODE_SUCCESS
	return nil
}

func print(args interface{}) {
	jsonByte, err := json.Marshal(args)
	if err != nil {
		return
	}
	logger.Debugf(string(jsonByte))
}
