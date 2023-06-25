package ipfshttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"

	"github.com/mtdepin/rep-mgr/config"

	ma "github.com/multiformats/go-multiaddr"
)

const configKey = "ipfshttp"
const envConfigKey = "cluster_ipfshttp"

// Default values for Config.
const (
	DefaultNodeAddr                = "/ip4/127.0.0.1/tcp/5001"
	DefaultConnectSwarmsDelay      = 30 * time.Second
	DefaultIPFSRequestTimeout      = 5 * time.Minute
	DefaultPinTimeout              = 2 * time.Minute
	DefaultUnpinTimeout            = 3 * time.Hour
	DefaultRepoGCTimeout           = 24 * time.Hour
	DefaultInformerTriggerInterval = 0 // disabled
	DefaultUnpinDisable            = false
)

// Config is used to initialize a Connector and allows to customize
// its behavior. It implements the config.ComponentConfig interface.
type Config struct {
	config.Saver

	// Host/Port for the IPFS daemon.
	NodeAddr ma.Multiaddr

	// ConnectSwarmsDelay specifies how long to wait after startup before
	// attempting to open connections from this peer's IPFS daemon to the
	// IPFS daemons of other peers.
	ConnectSwarmsDelay time.Duration

	// IPFS Daemon HTTP Client POST timeout
	IPFSRequestTimeout time.Duration

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

type jsonConfig struct {
	NodeMultiaddress        string `json:"node_multiaddress"`
	ConnectSwarmsDelay      string `json:"connect_swarms_delay"`
	IPFSRequestTimeout      string `json:"ipfs_request_timeout"`
	PinTimeout              string `json:"pin_timeout"`
	UnpinTimeout            string `json:"unpin_timeout"`
	RepoGCTimeout           string `json:"repogc_timeout"`
	InformerTriggerInterval int    `json:"informer_trigger_interval"`
	UnpinDisable            bool   `json:"unpin_disable,omitempty"`
}

// ConfigKey provides a human-friendly identifier for this type of Config.
func (cfg *Config) ConfigKey() string {
	return configKey
}

// Default sets the fields of this Config to sensible default values.
func (cfg *Config) Default() error {
	node, _ := ma.NewMultiaddr(DefaultNodeAddr)
	cfg.NodeAddr = node
	cfg.ConnectSwarmsDelay = DefaultConnectSwarmsDelay
	cfg.IPFSRequestTimeout = DefaultIPFSRequestTimeout
	cfg.PinTimeout = DefaultPinTimeout
	cfg.UnpinTimeout = DefaultUnpinTimeout
	cfg.RepoGCTimeout = DefaultRepoGCTimeout
	cfg.InformerTriggerInterval = DefaultInformerTriggerInterval
	cfg.UnpinDisable = DefaultUnpinDisable

	return nil
}

// ApplyEnvVars fills in any Config fields found
// as environment variables.
func (cfg *Config) ApplyEnvVars() error {
	jcfg, err := cfg.toJSONConfig()
	if err != nil {
		return err
	}

	err = envconfig.Process(envConfigKey, jcfg)
	if err != nil {
		return err
	}

	return cfg.applyJSONConfig(jcfg)
}

// Validate checks that the fields of this Config have sensible values,
// at least in appearance.
func (cfg *Config) Validate() error {
	var err error
	if cfg.NodeAddr == nil {
		err = errors.New("ipfshttp.node_multiaddress not set")
	}

	if cfg.ConnectSwarmsDelay < 0 {
		err = errors.New("ipfshttp.connect_swarms_delay is invalid")
	}

	if cfg.IPFSRequestTimeout < 0 {
		err = errors.New("ipfshttp.ipfs_request_timeout invalid")
	}

	if cfg.PinTimeout < 0 {
		err = errors.New("ipfshttp.pin_timeout invalid")
	}

	if cfg.UnpinTimeout < 0 {
		err = errors.New("ipfshttp.unpin_timeout invalid")
	}

	if cfg.RepoGCTimeout < 0 {
		err = errors.New("ipfshttp.repogc_timeout invalid")
	}
	if cfg.InformerTriggerInterval < 0 {
		err = errors.New("ipfshttp.update_metrics_after")
	}

	return err

}

// LoadJSON parses a JSON representation of this Config as generated by ToJSON.
func (cfg *Config) LoadJSON(raw []byte) error {
	jcfg := &jsonConfig{}
	err := json.Unmarshal(raw, jcfg)
	if err != nil {
		logger.Error("Error unmarshaling ipfshttp config")
		return err
	}

	cfg.Default()

	return cfg.applyJSONConfig(jcfg)
}

func (cfg *Config) applyJSONConfig(jcfg *jsonConfig) error {
	nodeAddr, err := ma.NewMultiaddr(jcfg.NodeMultiaddress)
	if err != nil {
		return fmt.Errorf("error parsing ipfs_node_multiaddress: %s", err)
	}

	cfg.NodeAddr = nodeAddr
	cfg.UnpinDisable = jcfg.UnpinDisable
	cfg.InformerTriggerInterval = jcfg.InformerTriggerInterval

	err = config.ParseDurations(
		"ipfshttp",
		&config.DurationOpt{Duration: jcfg.ConnectSwarmsDelay, Dst: &cfg.ConnectSwarmsDelay, Name: "connect_swarms_delay"},
		&config.DurationOpt{Duration: jcfg.IPFSRequestTimeout, Dst: &cfg.IPFSRequestTimeout, Name: "ipfs_request_timeout"},
		&config.DurationOpt{Duration: jcfg.PinTimeout, Dst: &cfg.PinTimeout, Name: "pin_timeout"},
		&config.DurationOpt{Duration: jcfg.UnpinTimeout, Dst: &cfg.UnpinTimeout, Name: "unpin_timeout"},
		&config.DurationOpt{Duration: jcfg.RepoGCTimeout, Dst: &cfg.RepoGCTimeout, Name: "repogc_timeout"},
	)
	if err != nil {
		return err
	}

	return cfg.Validate()
}

// ToJSON generates a human-friendly JSON representation of this Config.
func (cfg *Config) ToJSON() (raw []byte, err error) {
	jcfg, err := cfg.toJSONConfig()
	if err != nil {
		return
	}

	raw, err = config.DefaultJSONMarshal(jcfg)
	return
}

func (cfg *Config) toJSONConfig() (jcfg *jsonConfig, err error) {
	// Multiaddress String() may panic
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s", r)
		}
	}()

	jcfg = &jsonConfig{}

	// Set all configuration fields
	jcfg.NodeMultiaddress = cfg.NodeAddr.String()
	jcfg.ConnectSwarmsDelay = cfg.ConnectSwarmsDelay.String()
	jcfg.IPFSRequestTimeout = cfg.IPFSRequestTimeout.String()
	jcfg.PinTimeout = cfg.PinTimeout.String()
	jcfg.UnpinTimeout = cfg.UnpinTimeout.String()
	jcfg.RepoGCTimeout = cfg.RepoGCTimeout.String()
	jcfg.InformerTriggerInterval = cfg.InformerTriggerInterval
	jcfg.UnpinDisable = cfg.UnpinDisable

	return
}

// ToDisplayJSON returns JSON config as a string.
func (cfg *Config) ToDisplayJSON() ([]byte, error) {
	jcfg, err := cfg.toJSONConfig()
	if err != nil {
		return nil, err
	}

	return config.DisplayJSON(jcfg)
}
