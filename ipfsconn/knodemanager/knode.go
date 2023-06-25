package knodemanager

import (
	"context"
	"github.com/mtdepin/rep-mgr/api"
	"github.com/libp2p/go-libp2p/core/peer"
	"net/url"
	"strings"
	"time"
)

type IpfsIDResp struct {
	ID        peer.ID  `json:"id,omitempty" codec:"i,omitempty"`
	Addresses []string `json:"addresses" codec:"a,omitempty"`
	Error     string   `json:"error" codec:"e,omitempty"`
}

type Knode struct {
	PeerID    peer.ID
	Version   string
	Ip        string
	Port      string
	status    NodeStatus
	timestamp time.Time
	metrics   api.KnodeMetric
}

type PinArgs struct {
	Operation string
	Cid       string
	Param     url.Values
}

type PinResp struct {
	PinStatus api.IPFSPinStatus
	Pins      []api.IPFSPinInfo
}

type PinLsArgs struct {
	Operation string
	Param     url.Values
	Cid       string //如果为空就查询所有 pin ls

	//Stream    bool
	//Recursive bool
	//Type      string `json:"Type" codec:"type"` //all derct recursive, inderict

}

type PinLsResp struct {
	Pins []api.IPFSPinInfo
}

type RawIPFSPinInfo struct {
	PeerId  string    `json:"PeerId" codec:"peerId"`
	Cid     string    `json:"Cid" codec:"cid"`
	Type    string    `json:"Type" codec:"type"`
	Message string    `json:"Message" codec:"message"`
	Code    int       `json:"Code" codec:"code"`
	Pins    []string  `json:"Pins" codec:"pins"`
	Status  ErrorCode `json:"Status" codec:"status"`
}

func CopyRawIPFSPinInfoToIfaces(in []RawIPFSPinInfo) []interface{} {
	ifaces := make([]interface{}, len(in))
	for i := range in {
		in[i] = RawIPFSPinInfo{}
		ifaces[i] = &(in[i])
	}
	return ifaces
}

func (ri *RawIPFSPinInfo) defined() bool {
	return len(ri.Cid) > 0
}
func (ri *RawIPFSPinInfo) transToIPFSInfo() api.IPFSPinInfo {
	if !ri.defined() {
		return api.IPFSPinInfo{}
	}
	cid, _ := api.DecodeCid(ri.Cid)
	pid, err := peer.Decode(ri.PeerId)
	if err != nil {
		logger.Error(err)
	}

	pinInfo := api.IPFSPinInfo{
		Cid:    cid,
		PeerId: pid,
	}
	if ri.Type == "error" {
		if strings.HasSuffix(ri.Message, "not pinned") {
			pinInfo.Type = api.IPFSPinStatusUnpinned
		} else {
			pinInfo.Type = api.IPFSPinStatusError
		}
	} else {
		pinInfo.Type = api.IPFSPinStatusFromString(ri.Type)
	}

	return pinInfo
}

// ConnectPeers connect to knode swarm
func (k *Knode) ConnectPeers(ctx context.Context, km *KnodeManager, peers []string) error {
	var reply CommonResp
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := km.knodeRpcClient.CallContext(ctx, k.PeerID, "KNodeService", "SwarmConnect", peers, &reply)
	if err != nil {
		logger.Errorf("knode SwarmConnect: %s, error: %s", k.PeerID.String(), err)
		return err
	}
	return nil
}

func (k *Knode) valid() bool {
	return k.status == Valid
}
