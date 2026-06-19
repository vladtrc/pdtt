package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/vladtrc/pdtt/internal/secret"
	"gopkg.in/yaml.v3"
)

const (
	defaultPort              = 8080
	defaultMySQLDSN          = "pdttweb:CHANGE_ME@tcp(127.0.0.1:3306)/pdttweb?parseTime=true&charset=utf8mb4"
	defaultDataDir           = "./data"
	maxRenderTimeout         = 3 * time.Minute
	defaultRenderTimeout     = maxRenderTimeout
	defaultRetention         = 24 * time.Hour
	defaultMaxSceneBytes     = 1 << 20 // 1 MiB
	defaultLeaseDuration     = 2 * time.Minute
	defaultWorkerPoll        = 2 * time.Second
	defaultCleanupEvery      = 10 * time.Minute
	defaultOpenRouterTimeout = time.Minute
)

type Config struct {
	Port          int           `yaml:"port"`
	MySQL         MySQL         `yaml:"mysql"`
	DataDir       string        `yaml:"data_dir"`
	RenderTimeout time.Duration `yaml:"render_timeout"`
	Retention     time.Duration `yaml:"retention"`
	MaxSceneBytes int64         `yaml:"max_scene_bytes"`
	AdminSecret   string        `yaml:"admin_secret"`
	LeaseDuration time.Duration `yaml:"lease_duration"`
	WorkerPoll    time.Duration `yaml:"worker_poll"`
	CleanupEvery  time.Duration `yaml:"cleanup_every"`
	OpenRouter    OpenRouter    `yaml:"openrouter"`
}

type MySQL struct {
	DSN string `yaml:"dsn"`
}

type OpenRouter struct {
	Key       string        `yaml:"key"`
	KeyEnc    string        `yaml:"key_enc"`
	BaseURL   string        `yaml:"base_url"`
	Model     string        `yaml:"model"`
	RulesPath string        `yaml:"rules_path"`
	Timeout   time.Duration `yaml:"timeout"`
}

func Load(path, secretPath string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if cfg.OpenRouter.KeyEnc != "" && cfg.OpenRouter.Key == "" {
		secretB64, err := readSecretFile(secretPath)
		if err != nil {
			return nil, fmt.Errorf("read .secret: %w", err)
		}
		plaintext, err := secret.DecryptWithKey(cfg.OpenRouter.KeyEnc, secretB64)
		if err != nil {
			return nil, fmt.Errorf("decrypt openrouter key: %w", err)
		}
		cfg.OpenRouter.Key = plaintext
	}

	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	if strings.TrimSpace(cfg.MySQL.DSN) == "" {
		cfg.MySQL.DSN = defaultMySQLDSN
	}
	if strings.TrimSpace(cfg.DataDir) == "" {
		cfg.DataDir = defaultDataDir
	}
	if cfg.RenderTimeout == 0 {
		cfg.RenderTimeout = defaultRenderTimeout
	}
	if cfg.RenderTimeout > maxRenderTimeout {
		cfg.RenderTimeout = maxRenderTimeout
	}
	if cfg.Retention == 0 {
		cfg.Retention = defaultRetention
	}
	if cfg.MaxSceneBytes <= 0 {
		cfg.MaxSceneBytes = defaultMaxSceneBytes
	}
	if cfg.LeaseDuration == 0 {
		cfg.LeaseDuration = defaultLeaseDuration
	}
	if cfg.WorkerPoll == 0 {
		cfg.WorkerPoll = defaultWorkerPoll
	}
	if cfg.CleanupEvery == 0 {
		cfg.CleanupEvery = defaultCleanupEvery
	}
	if cfg.OpenRouter.BaseURL == "" {
		cfg.OpenRouter.BaseURL = "https://openrouter.ai/api/v1"
	}
	if cfg.OpenRouter.Model == "" {
		cfg.OpenRouter.Model = "deepseek/deepseek-v4-flash"
	}
	if cfg.OpenRouter.Timeout == 0 {
		cfg.OpenRouter.Timeout = defaultOpenRouterTimeout
	}
}

func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", c.Port)
	}
	if strings.TrimSpace(c.MySQL.DSN) == "" {
		return fmt.Errorf("mysql.dsn is required")
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return fmt.Errorf("data_dir is required")
	}
	if c.RenderTimeout < time.Second {
		return fmt.Errorf("render_timeout must be at least 1s")
	}
	if c.Retention < time.Minute {
		return fmt.Errorf("retention must be at least 1m")
	}
	if c.MaxSceneBytes < 1024 {
		return fmt.Errorf("max_scene_bytes must be at least 1024")
	}
	if strings.TrimSpace(c.AdminSecret) == "" {
		return fmt.Errorf("admin_secret is required")
	}
	if strings.ContainsAny(c.AdminSecret, "/?#") {
		return fmt.Errorf("admin_secret must be URL-safe (no /, ?, or #)")
	}
	if c.LeaseDuration < 5*time.Second {
		return fmt.Errorf("lease_duration must be at least 5s")
	}
	if c.WorkerPoll < 100*time.Millisecond {
		return fmt.Errorf("worker_poll must be at least 100ms")
	}
	if c.CleanupEvery < time.Minute {
		return fmt.Errorf("cleanup_every must be at least 1m")
	}
	if c.LeaseDuration < c.WorkerPoll {
		return fmt.Errorf("lease_duration must be >= worker_poll")
	}
	if c.OpenRouter.Timeout != 0 && c.OpenRouter.Timeout < 5*time.Second {
		return fmt.Errorf("openrouter.timeout must be at least 5s")
	}
	return nil
}

func readSecretFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
