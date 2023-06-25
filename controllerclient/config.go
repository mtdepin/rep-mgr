package controllerclient

import (
	"encoding/json"
	"github.com/mtdepin/rep-mgr/config"
)

const configKey = "controller"

type Config struct {
	config.Saver

	Url string
}

func (c *Config) ConfigKey() string {
	return configKey
}

func (c *Config) LoadJSON(raw []byte) error {

	//jcfg := &Config{}
	//err := json.Unmarshal(raw, jcfg)
	//if err != nil {
	//	logger.Error("Error unmarshaling cluster config")
	//	return err
	//}

	return json.Unmarshal(raw, c)
}

func (c *Config) ToJSON() ([]byte, error) {
	return json.Marshal(c)
}

func (c *Config) Default() error {
	c.Url = ""
	return nil
}

func (c *Config) ApplyEnvVars() error {
	return nil
}

func (c *Config) Validate() error {
	return nil
}

func (c *Config) ToDisplayJSON() ([]byte, error) {
	return config.DisplayJSON(c)
}
