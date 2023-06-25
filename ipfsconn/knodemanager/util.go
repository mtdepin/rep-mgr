package knodemanager

import (
	"github.com/mtdepin/rep-mgr/api"
	ma "github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
	"net"
)

func publicIPFSAddresses(in []api.Multiaddr) []api.Multiaddr {
	var out []api.Multiaddr
	for _, maddr := range in {
		if madns.Matches(maddr.Value()) { // a dns multiaddress: take it
			out = append(out, maddr)
			continue
		}

		ip, err := maddr.ValueForProtocol(ma.P_IP4)
		if err != nil {
			ip, err = maddr.ValueForProtocol(ma.P_IP6)
			if err != nil {
				continue
			}
		}
		// We have an IP in the multiaddress. Only include
		// global unicast.
		netip := net.ParseIP(ip)
		if netip == nil {
			continue
		}

		if !netip.IsGlobalUnicast() {
			continue
		}
		out = append(out, maddr)
	}
	return out
}
