package disk

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/mtdepin/rep-mgr/config"
	"github.com/kelseyhightower/envconfig"
)

const configKey = "disk"
const envConfigKey = "cluster_disk"

// Default values for disk Config
const (
	DefaultMetricTTL  = 30 * time.Second
	DefaultMetricType = MetricFreeSpace
)

// Config is used to initialize an Informer and customize
// the type and parameters of the metric it produces.
type Config struct {
	config.Saver

	MetricTTL  time.Duration
	MetricType MetricType
}

type jsonConfig struct {
	MetricTTL  string `json:"metric_ttl"`
	MetricType string `json:"metric_type"`
}

// ConfigKey returns a human-friendly identifier for this type of Metric.
func (cfg *Config) ConfigKey() string {
	return configKey
}

// Default initializes this Config with sensible values.
func (cfg *Config) Default() error {
	cfg.MetricTTL = DefaultMetricTTL
	cfg.MetricType = DefaultMetricType
	return nil
}

// ApplyEnvVars fills in any Config fields found
// as environment variables.
func (cfg *Config) ApplyEnvVars() error {
	jcfg := cfg.toJSONConfig()

	err := envconfig.Process(envConfigKey, jcfg)
	if err != nil {
		return err
	}

	return cfg.applyJSONConfig(jcfg)
}

// Validate checks that the fields of this Config have working values,
// at least in appearance.
func (cfg *Config) Validate() error {
	if cfg.MetricTTL <= 0 {
		return errors.New("disk.metric_ttl is invalid")
	}

	if cfg.MetricType.String() == "" {
		return errors.New("disk.metric_type is invalid")
	}
	return nil
}

// LoadJSON reads the fields of this Config from a JSON byteslice as
// generated by ToJSON.
func (cfg *Config) LoadJSON(raw []byte) error {
	jcfg := &jsonConfig{}
	err := json.Unmarshal(raw, jcfg)
	if err != nil {
		logger.Error("Error unmarshaling disk informer config")
		return err
	}

	cfg.Default()

	return cfg.applyJSONConfig(jcfg)
}

func (cfg *Config) applyJSONConfig(jcfg *jsonConfig) error {
	t, _ := time.ParseDuration(jcfg.MetricTTL)
	cfg.MetricTTL = t

	switch jcfg.MetricType {
	case "reposize":
		cfg.MetricType = MetricRepoSize
	case "freespace":
		cfg.MetricType = MetricFreeSpace
	default:
		return errors.New("disk.metric_type is invalid")
	}

	return cfg.Validate()
}

// ToJSON generates a JSON-formatted human-friendly representation of this
// Config.
func (cfg *Config) ToJSON() (raw []byte, err error) {
	jcfg := cfg.toJSONConfig()

	raw, err = config.DefaultJSONMarshal(jcfg)
	return
}

func (cfg *Config) toJSONConfig() *jsonConfig {
	return &jsonConfig{
		MetricTTL:  cfg.MetricTTL.String(),
		MetricType: cfg.MetricType.String(),
	}
}

// ToDisplayJSON returns JSON config as a string.
func (cfg *Config) ToDisplayJSON() ([]byte, error) {
	return config.DisplayJSON(cfg.toJSONConfig())
}
