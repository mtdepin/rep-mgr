package mock

import (
	"context"
	"fmt"
	"github.com/mtdepin/rep-mgr/api"
	"github.com/mtdepin/rep-mgr/ipfsconn/knodemanager"
	"github.com/libp2p/go-libp2p"
	gorpc "github.com/libp2p/go-libp2p-gorpc"
	host "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
	"log"
	"time"
)

var protocolID = protocol.ID("/p2p/rpc/rm")

func startDaemon() {
	//decodeString, err := base64.StdEncoding.DecodeString("CAESQL/edkoe6bpjiRevXVJL1hWywl76FFIVwt8k54CA1LbhCHspKK0B1SF0ZpG0Lid/HPbzT9qy5wP33RtDq2J7g2s=")
	//if err != nil {
	//	panic(err)
	//}

	//pri, err := crypto.UnmarshalPrivateKey(decodeString)
	client, err := libp2p.New(
		libp2p.NoListenAddrs,
		//libp2p.Identity(pri),
		//libp2p.EnableRelay(),
	)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Hello , my hosts ID is %s\n", client.ID().String())

	host := "/ip4/127.0.0.1/udp/9500/quic/p2p/12D3KooWHiXbnwUpnh7wzPX2vHaLq9QdQHpzSuYmM9PB97RFSSc7"
	//host := "/ip4/183.223.252.54/udp/29500/quic/p2p/12D3KooWJgry27uFGR4V6AB5kzyUurpMbgeHMKxtNhkb4ZAn2aj7"
	//rpcHost := gorpc.NewServer(client, protocolID)

	//start a rpc server and register rpc service
	rpcHost := gorpc.NewServer(client, protocolID)
	svc := KNodeService{}
	err = rpcHost.Register(&svc)
	if err != nil {
		panic(err)
	}

	ma, err := multiaddr.NewMultiaddr(host)
	if err != nil {
		panic(err)
	}
	peerInfo, err := peer.AddrInfoFromP2pAddr(ma)
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	err = client.Connect(ctx, *peerInfo)
	if err != nil {
		panic(err)
	}
	rpcClient := gorpc.NewClient(client, protocolID)

	regist(client, peerInfo, rpcClient)
	sendMetrice(client, peerInfo, rpcClient)

}

func regist(client host.Host, peerInfo *peer.AddrInfo, rpcClient *gorpc.Client) {
	var reply knodemanager.CommonResp
	var args knodemanager.RegisArgs
	err := rpcClient.Call(peerInfo.ID, "RMService", "Regist", args, &reply)
	if err != nil {
		fmt.Println(err)
	}
}

func sendMetrice(client host.Host, peerInfo *peer.AddrInfo, rpcClient *gorpc.Client) {
	for {
		var reply knodemanager.CommonResp
		var args = api.KnodeMetric{
			PeerID:   client.ID(),
			Value:    100000000,
			Expire:   time.Now().Add(10 * time.Second),
			Valid:    true,
			Weight:   100000000,
			Address:  "http://127.0.0.1:9500/path/to/upload",
			Netspeed: 100,
		}
		fmt.Println("sendMetrice:  ", args)
		err := rpcClient.Call(peerInfo.ID, "RMService", "PushMetric", args, &reply)
		if err != nil {
			fmt.Println(err)
		}
		time.Sleep(5 * time.Second)

	}
}

type KNodeService struct{}

func (t *KNodeService) ID(ctx context.Context, in string, out *api.IPFSID) error {

	addr, err := api.NewMultiaddr("/ip4/10.80.51.53/tcp/4001/p2p/12D3KooWNz3gcjwirdUhteGbrAi92KKQEfAwPLmmQTyim1ZAShSq")
	if err != nil {
		return err
	}
	id, err := peer.IDFromBytes([]byte("12D3KooWNz3gcjwirdUhteGbrAi92KKQEfAwPLmmQTyim1ZAShSq"))
	out.ID = id
	out.Addresses = []api.Multiaddr{addr}

	return err
}

func (t *KNodeService) Pin(ctx context.Context, argType knodemanager.PinArgs, resp *knodemanager.RawIPFSPinInfo) error {
	log.Println("Received a pin call")
	log.Println("cid: ", argType.Cid)

	resp.Cid = argType.Cid
	resp.Status = knodemanager.ErrorCode(api.IPFSPinStatusRecursive)
	return nil
}

func (t *KNodeService) PinLs(ctx context.Context, argType knodemanager.PinLsArgs, resp *knodemanager.PinLsResp) error {
	log.Println("Received a PinLs call")

	return nil
}

func (t *KNodeService) PinLsCid(ctx context.Context, argType knodemanager.PinLsArgs, resp *knodemanager.PinResp) error {
	log.Println("Received a PinLs call")

	resp.PinStatus = api.IPFSPinStatusRecursive
	return nil
}

func (t *KNodeService) SwarmConnect(ctx context.Context, origins []api.Multiaddr, resp *knodemanager.CommonResp) error {
	log.Println("Received a SwarmConnect call")
	log.Println(origins)
	return nil
}
