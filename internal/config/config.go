package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	Cluster  ClusterConfig  `yaml:"cluster"`
}

type ServerConfig struct {
	Listen     string `yaml:"listen"`
	V6Listen   string `yaml:"v6_listen"`
	Interface  string `yaml:"interface"`
	HTTPListen string `yaml:"http_listen"`
}

type DatabaseConfig struct {
	URL      string `yaml:"url"`
	MaxConns int32  `yaml:"max_conns"`
}

type AuthConfig struct {
	RSAPrivateKeyFile    string        `yaml:"rsa_private_key"`
	RSAPublicKeyFile     string        `yaml:"rsa_public_key"`
	TokenTTL             time.Duration `yaml:"token_ttl"`
	DefaultAdminUsername string        `yaml:"default_admin_username"`
	DefaultAdminPassword string        `yaml:"default_admin_password"`
}

type ClusterConfig struct {
	Enabled            bool          `yaml:"enabled"`
	ClusterID          string        `yaml:"cluster_id"`
	NodeID             string        `yaml:"node_id"`
	ListenAddr         string        `yaml:"listen_addr"`
	HeartbeatInterval  time.Duration `yaml:"heartbeat_interval"`
	NodeTimeout        time.Duration `yaml:"node_timeout"`
	DiscoverReplyDelay time.Duration `yaml:"discover_reply_delay"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	setDefaults(&cfg)
	if err := validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func setDefaults(cfg *Config) {
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = "0.0.0.0:67"
	}
	if cfg.Server.V6Listen == "" {
		cfg.Server.V6Listen = "[::]:547"
	}
	if cfg.Server.HTTPListen == "" {
		cfg.Server.HTTPListen = "0.0.0.0:8080"
	}
	if cfg.Database.MaxConns == 0 {
		cfg.Database.MaxConns = 20
	}
	if cfg.Auth.TokenTTL == 0 {
		cfg.Auth.TokenTTL = 24 * time.Hour
	}
	if cfg.Cluster.HeartbeatInterval == 0 {
		cfg.Cluster.HeartbeatInterval = 5 * time.Second
	}
	if cfg.Cluster.NodeTimeout == 0 {
		cfg.Cluster.NodeTimeout = 30 * time.Second
	}
	if cfg.Cluster.ClusterID == "" {
		cfg.Cluster.ClusterID = "default"
	}
	if cfg.Cluster.DiscoverReplyDelay < 0 {
		cfg.Cluster.DiscoverReplyDelay = 0
	}
}

func validate(cfg *Config) error {
	if cfg.Database.URL == "" {
		return fmt.Errorf("database.url is required")
	}
	if cfg.Auth.RSAPrivateKeyFile == "" {
		return fmt.Errorf("auth.rsa_private_key is required")
	}
	if cfg.Auth.RSAPublicKeyFile == "" {
		return fmt.Errorf("auth.rsa_public_key is required")
	}
	if cfg.Cluster.Enabled {
		if cfg.Cluster.NodeID == "" {
			return fmt.Errorf("cluster.node_id is required when cluster is enabled")
		}
		if cfg.Cluster.ListenAddr == "" {
			cfg.Cluster.ListenAddr = cfg.Server.Listen
		}
	}
	return nil
}
