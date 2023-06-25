package api

import (
	"fmt"
	peer "github.com/libp2p/go-libp2p/core/peer"
	"strings"
)

// PeersToStrings Encodes a list of peers.
func PeersToStrings(peers []peer.ID) []string {
	strs := make([]string, len(peers))
	for i, p := range peers {
		if p != "" {
			//strs[i] = p.String()
			strs[i] = string(p)
		}
	}
	return strs
}

// StringsToPeers decodes peer.IDs from strings.
func StringsToPeers(strs []string) []peer.ID {
	peers := []peer.ID{}
	for _, p := range strs {
		//pid, err := peer.Decode(p)
		//if err != nil {
		//	continue
		//}
		pid := peer.ID(p)

		peers = append(peers, pid)
	}
	return peers
}

func ParseRmPeersFromAllocator(strs []peer.ID) ([]peer.ID, error) {
	peers := []peer.ID{}
	for _, p := range strs {
		rmId, _, err := ParseAllocate(string(p))
		if err != nil {
			return peers, err
		}

		//判断去重
		isexist := false
		for _, p := range peers {
			if p == rmId {
				isexist = true
				break
			}
		}
		if !isexist {
			peers = append(peers, rmId)
		}
	}
	return peers, nil
}

func ParseAllocate(allocateID string) (rmId peer.ID, knodeId peer.ID, err error) {

	peers := strings.Split(allocateID, ".")
	if len(peers) != 2 {
		return "", "", fmt.Errorf("invalid allocator id: %s", allocateID)
	}
	rmId, err = peer.Decode(peers[0])
	knodeId, err = peer.Decode(peers[1])

	return rmId, knodeId, nil

}
