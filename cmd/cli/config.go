package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

type App struct {
	Config     *Config
	ConfigPath string
	Output     string
	Quiet      bool
	Verbose    bool
}

type Config struct {
	HTTPBaseURL string `json:"http_base_url"`
	TCPAddr     string `json:"tcp_addr"`
	UDPAddr     string `json:"udp_addr"`
	GRPCAddr    string `json:"grpc_addr"`
	WSURL       string `json:"ws_url"`
	Token       string `json:"token,omitempty"`
	Username    string `json:"username,omitempty"`
}

type GlobalOptions struct {
	ConfigPath string
	Output     string
	Quiet      bool
	Verbose    bool
}

var (
	Version   = "dev"
	GoVersion = "unknown"
)

func parseGlobalFlags(args []string) (GlobalOptions, []string, error) {
	opts := GlobalOptions{Output: "text"}
	fs := flag.NewFlagSet("mangahub", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&opts.ConfigPath, "config", "", "config path")
	fs.StringVar(&opts.Output, "output", "text", "output format: text or json")
	fs.BoolVar(&opts.Verbose, "verbose", false, "enable verbose output")
	fs.BoolVar(&opts.Quiet, "quiet", false, "suppress non-essential output")
	if err := fs.Parse(args); err != nil {
		return opts, nil, err
	}
	remaining := fs.Args()
	return opts, remaining, nil
}

func LoadConfig(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := DefaultConfig()
			return cfg, nil
		}
		return nil, err
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func SaveConfig(path string, cfg *Config) error {
	if path == "" {
		path = DefaultConfigPath()
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "./.mangahub/config.json"
	}
	return filepath.Join(home, ".mangahub", "config.json")
}

func DefaultConfig() *Config {
	return &Config{
		HTTPBaseURL: "http://localhost:8080",
		TCPAddr:     "localhost:9090",
		UDPAddr:     "127.0.0.1:7071",
		GRPCAddr:    "localhost:9092",
		WSURL:       "ws://localhost:8080/ws",
	}
}

func defaultLogsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "./.mangahub/logs"
	}
	return filepath.Join(home, ".mangahub", "logs")
}

func ensureDir(path string) error {
	if path == "" {
		return fmt.Errorf("missing directory")
	}
	return os.MkdirAll(path, 0o755)
}
