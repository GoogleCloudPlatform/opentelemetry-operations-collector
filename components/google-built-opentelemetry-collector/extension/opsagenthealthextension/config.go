package opsagenthealthextension

type Config struct {
	ConfigPath string `mapstructure:"config_path"`
	OutDir     string `mapstructure:"out_dir"`
}
