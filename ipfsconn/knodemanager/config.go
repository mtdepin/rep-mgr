package knodemanager

import (
	"github.com/mtdepin/rep-mgr/config"
	ma "github.com/multiformats/go-multiaddr"
	"time"
)

// Config is used to initialize a Connector and allows to customize
// its behavior. It implements the config.ComponentConfig interface.
type Config struct {
	config.Saver

	// Host/Port for the IPFS daemon.
	NodeAddr []ma.Multiaddr

	// ConnectSwarmsDelay specifies how long to wait after startup before
	// attempting to open connections from this peer's IPFS daemon to the
	// IPFS daemons of other peers.
	ConnectSwarmsDelay time.Duration

	// knode Daemon HTTP Client POST timeout
	KnodeRequestTimeout time.Duration

	// Pin Operation timeout
	PinTimeout time.Duration

	// Unpin Operation timeout
	UnpinTimeout time.Duration

	// RepoGC Operation timeout
	RepoGCTimeout time.Duration

	// How many pin and block/put operations need to happen before we do a
	// special broadcast informer metrics to the network. 0 to disable.
	InformerTriggerInterval int

	// Disables the unpin operation and returns an error.
	UnpinDisable bool

	// Tracing flag used to skip tracing specific paths when not enabled.
	Tracing bool
}

func DefaultConfig() Config {
	return Config{
		KnodeRequestTimeout: 10 * time.Second,
		UnpinTimeout:        60 * time.Second,
		ConnectSwarmsDelay:  15 * time.Second,
	}
}
