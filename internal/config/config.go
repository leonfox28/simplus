package config

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	BackendSimulator = "simulator"
	BackendHardware  = "hardware"
	BackendReplay    = "replay"
)

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Storage StorageConfig `yaml:"storage"`
	Runtime RuntimeConfig `yaml:"runtime"`
}

type ServerConfig struct {
	Listen string `yaml:"listen"`
}

type StorageConfig struct {
	DataRoot string `yaml:"data_root"`
}

type RuntimeConfig struct {
	Backend     string `yaml:"backend"`
	AgentSocket string `yaml:"agent_socket"`
}

func Defaults() Config {
	return Config{
		Server:  ServerConfig{Listen: "127.0.0.1:8080"},
		Storage: StorageConfig{DataRoot: filepath.Join(".dev", "data")},
		Runtime: RuntimeConfig{Backend: BackendSimulator, AgentSocket: "/run/simplus/simplus-agent.sock"},
	}
}

func Load(path string) (Config, error) {
	cfg := Defaults()
	defaultDataRoot, err := filepath.Abs(cfg.Storage.DataRoot)
	if err != nil {
		return Config{}, fmt.Errorf("resolve default storage.data_root: %w", err)
	}
	cfg.Storage.DataRoot = defaultDataRoot

	if path != "" {
		file, err := os.Open(path)
		if err != nil {
			return Config{}, fmt.Errorf("open config: %w", err)
		}
		defer file.Close()

		decoder := yaml.NewDecoder(file)
		decoder.KnownFields(true)
		if err := decoder.Decode(&cfg); err != nil {
			return Config{}, fmt.Errorf("decode config: %w", err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			if err != nil {
				return Config{}, fmt.Errorf("decode trailing config: %w", err)
			}
			return Config{}, errors.New("config must contain exactly one YAML document")
		}
	}

	applyEnvironment(&cfg)
	if strings.TrimSpace(cfg.Storage.DataRoot) == "" {
		return Config{}, errors.New("storage.data_root must not be empty")
	}
	if !filepath.IsAbs(cfg.Storage.DataRoot) {
		if os.Getenv("SIMPLUS_DATA_ROOT") != "" {
			return Config{}, errors.New("SIMPLUS_DATA_ROOT must be an absolute path")
		}
		if path == "" {
			return Config{}, errors.New("storage.data_root must be absolute when no config file is used")
		}
		configPath, err := filepath.Abs(path)
		if err != nil {
			return Config{}, fmt.Errorf("resolve config path: %w", err)
		}
		cfg.Storage.DataRoot = filepath.Join(filepath.Dir(configPath), cfg.Storage.DataRoot)
	}
	cfg.Storage.DataRoot = filepath.Clean(cfg.Storage.DataRoot)
	if filepath.IsAbs(cfg.Runtime.AgentSocket) {
		cfg.Runtime.AgentSocket = filepath.Clean(cfg.Runtime.AgentSocket)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyEnvironment(cfg *Config) {
	if value := os.Getenv("SIMPLUS_LISTEN_ADDR"); value != "" {
		cfg.Server.Listen = value
	}
	if value := os.Getenv("SIMPLUS_DATA_ROOT"); value != "" {
		cfg.Storage.DataRoot = value
	}
	if value := os.Getenv("SIMPLUS_BACKEND"); value != "" {
		cfg.Runtime.Backend = value
	}
	if value := os.Getenv("SIMPLUS_AGENT_SOCKET"); value != "" {
		cfg.Runtime.AgentSocket = value
	}
}

func (cfg Config) Validate() error {
	if strings.TrimSpace(cfg.Server.Listen) == "" {
		return errors.New("server.listen must not be empty")
	}
	host, portText, err := net.SplitHostPort(cfg.Server.Listen)
	if err != nil {
		return fmt.Errorf("server.listen: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("server.listen port must be an integer from 1 through 65535")
	}
	ip := net.ParseIP(host)
	isIPv4Wildcard := ip != nil && ip.To4() != nil && ip.Equal(net.IPv4zero)
	if ip == nil || (!isIPv4Wildcard && !ip.IsLoopback() && !ip.IsPrivate()) {
		return fmt.Errorf("server.listen must use a numeric loopback, private LAN address, or IPv4 wildcard: %q", cfg.Server.Listen)
	}

	if strings.TrimSpace(cfg.Storage.DataRoot) == "" {
		return errors.New("storage.data_root must not be empty")
	}
	if !filepath.IsAbs(cfg.Storage.DataRoot) {
		return errors.New("storage.data_root must resolve to an absolute path")
	}
	if filepath.Clean(cfg.Storage.DataRoot) == string(filepath.Separator) {
		return errors.New("storage.data_root must not be the filesystem root")
	}

	switch cfg.Runtime.Backend {
	case BackendSimulator, BackendHardware, BackendReplay:
	default:
		return fmt.Errorf("runtime.backend must be simulator, hardware, or replay: %q", cfg.Runtime.Backend)
	}
	if cfg.Runtime.Backend == BackendHardware {
		if strings.TrimSpace(cfg.Runtime.AgentSocket) == "" || !filepath.IsAbs(cfg.Runtime.AgentSocket) {
			return errors.New("runtime.agent_socket must be an absolute path for the hardware backend")
		}
	}
	return nil
}
