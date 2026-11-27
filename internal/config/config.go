package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds the full application configuration loaded via Viper.
type Config struct {
	App          AppConfig          `mapstructure:"app"`
	Server       ServerConfig       `mapstructure:"server"`
	Database     DatabaseConfig     `mapstructure:"database"`
	GitHub       GitHubConfig       `mapstructure:"github"`
	Security     SecurityConfig     `mapstructure:"security"`
	Admin        AdminConfig        `mapstructure:"admin"`
	Moderation   ModerationConfig   `mapstructure:"moderation"`
	Orchestrator OrchestratorConfig `mapstructure:"orchestrator"`
	Logging      LoggingConfig      `mapstructure:"logging"`
}

type AppConfig struct {
	Name        string `mapstructure:"name"`
	Environment string `mapstructure:"environment"` // development | production
}

type ServerConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	BodyLimitBytes  int           `mapstructure:"body_limit_bytes"`
	AllowedOrigins  []string      `mapstructure:"allowed_origins"`
	TrustedProxies  []string      `mapstructure:"trusted_proxies"`
}

type DatabaseConfig struct {
	URL             string        `mapstructure:"url"`
	MaxConns        int32         `mapstructure:"max_conns"`
	MinConns        int32         `mapstructure:"min_conns"`
	MaxConnLifetime time.Duration `mapstructure:"max_conn_lifetime"`
	MaxConnIdleTime time.Duration `mapstructure:"max_conn_idle_time"`
}

type GitHubConfig struct {
	AppID          int64  `mapstructure:"app_id"`
	AppSlug        string `mapstructure:"app_slug"`
	ClientID       string `mapstructure:"client_id"`
	WebhookSecret  string `mapstructure:"webhook_secret"`
	PrivateKeyPEM  string `mapstructure:"private_key_pem"`
	PrivateKeyPath string `mapstructure:"private_key_path"`
	APIBaseURL     string `mapstructure:"api_base_url"`
}

type SecurityConfig struct {
	SessionSecret      string        `mapstructure:"session_secret"`
	EncryptionKey      string        `mapstructure:"encryption_key"`
	SessionTTL         time.Duration `mapstructure:"session_ttl"`
	CookieSecure       bool          `mapstructure:"cookie_secure"`
	CookieSameSite     string        `mapstructure:"cookie_same_site"`
	LoginRateMax       int           `mapstructure:"login_rate_max"`
	WebhookRateMax     int           `mapstructure:"webhook_rate_max"`
	EphemeralKeysInDev bool          `mapstructure:"-"`
}

type AdminConfig struct {
	Email    string `mapstructure:"email"`
	Password string `mapstructure:"password"`
	Name     string `mapstructure:"name"`
}

type ModerationConfig struct {
	DefaultAction string `mapstructure:"default_action"` // allow | require_approval | deny
}

type OrchestratorConfig struct {
	Provider          string        `mapstructure:"provider"` // simulated | docker
	DockerSocket      string        `mapstructure:"docker_socket"`
	DefaultImage      string        `mapstructure:"default_image"`
	WorkerCount       int           `mapstructure:"worker_count"`
	QueueSize         int           `mapstructure:"queue_size"`
	ReconcileInterval time.Duration `mapstructure:"reconcile_interval"`
	ProvisionTimeout  time.Duration `mapstructure:"provision_timeout"`
	StaleHeartbeatTTL time.Duration `mapstructure:"stale_heartbeat_ttl"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"` // json | console
}

// Load reads configuration from optional config file, defaults, and RUNWAY_* env vars.
func Load(configPath string) (*Config, error) {
	v := viper.New()
	setDefaults(v)

	v.SetEnvPrefix("RUNWAY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	bindEnvKeys(v)

	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config file %q: %w", configPath, err)
		}
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("/etc/runway")
		if err := v.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				return nil, fmt.Errorf("read config: %w", err)
			}
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if err := cfg.validateAndNormalize(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("app.name", "runway")
	v.SetDefault("app.environment", "development")

	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.read_timeout", "15s")
	v.SetDefault("server.write_timeout", "30s")
	v.SetDefault("server.idle_timeout", "60s")
	v.SetDefault("server.shutdown_timeout", "10s")
	v.SetDefault("server.body_limit_bytes", 2*1024*1024)
	v.SetDefault("server.allowed_origins", []string{})
	v.SetDefault("server.trusted_proxies", []string{})

	v.SetDefault("database.max_conns", 20)
	v.SetDefault("database.min_conns", 2)
	v.SetDefault("database.max_conn_lifetime", "30m")
	v.SetDefault("database.max_conn_idle_time", "5m")

	v.SetDefault("github.app_slug", "runway")
	v.SetDefault("github.api_base_url", "https://api.github.com")

	v.SetDefault("security.session_ttl", "24h")
	v.SetDefault("security.cookie_secure", false)
	v.SetDefault("security.cookie_same_site", "Lax")
	v.SetDefault("security.login_rate_max", 10)
	v.SetDefault("security.webhook_rate_max", 120)

	v.SetDefault("admin.email", "admin@runway.local")
	v.SetDefault("admin.name", "Runway Admin")

	v.SetDefault("moderation.default_action", "allow")

	v.SetDefault("orchestrator.provider", "simulated")
	v.SetDefault("orchestrator.docker_socket", "/var/run/docker.sock")
	v.SetDefault("orchestrator.default_image", "ghcr.io/actions/actions-runner:latest")
	v.SetDefault("orchestrator.worker_count", 4)
	v.SetDefault("orchestrator.queue_size", 128)
	v.SetDefault("orchestrator.reconcile_interval", "30s")
	v.SetDefault("orchestrator.provision_timeout", "3m")
	v.SetDefault("orchestrator.stale_heartbeat_ttl", "5m")

	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
}

// bindEnvKeys explicitly binds all nested config keys so Viper Unmarshal picks up env vars
// even when no config file is present.
func bindEnvKeys(v *viper.Viper) {
	keys := []string{
		"app.name", "app.environment",
		"server.host", "server.port", "server.read_timeout", "server.write_timeout",
		"server.idle_timeout", "server.shutdown_timeout", "server.body_limit_bytes",
		"server.allowed_origins", "server.trusted_proxies",
		"database.url", "database.max_conns", "database.min_conns",
		"database.max_conn_lifetime", "database.max_conn_idle_time",
		"github.app_id", "github.app_slug", "github.client_id",
		"github.webhook_secret", "github.private_key_pem", "github.private_key_path",
		"github.api_base_url",
		"security.session_secret", "security.encryption_key", "security.session_ttl",
		"security.cookie_secure", "security.cookie_same_site",
		"security.login_rate_max", "security.webhook_rate_max",
		"admin.email", "admin.password", "admin.name",
		"moderation.default_action",
		"orchestrator.provider", "orchestrator.docker_socket", "orchestrator.default_image",
		"orchestrator.worker_count", "orchestrator.queue_size",
		"orchestrator.reconcile_interval", "orchestrator.provision_timeout",
		"orchestrator.stale_heartbeat_ttl",
		"logging.level", "logging.format",
	}
	for _, k := range keys {
		_ = v.BindEnv(k)
	}
}

func (c *Config) validateAndNormalize() error {
	c.App.Environment = strings.ToLower(strings.TrimSpace(c.App.Environment))
	if c.App.Environment != "development" && c.App.Environment != "production" {
		return fmt.Errorf("invalid app.environment %q: must be 'development' or 'production'", c.App.Environment)
	}

	if strings.TrimSpace(c.Database.URL) == "" {
		return fmt.Errorf("database.url (RUNWAY_DATABASE_URL) is required")
	}

	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server.port %d", c.Server.Port)
	}

	if c.App.Environment == "production" {
		if len(strings.TrimSpace(c.Security.SessionSecret)) < 32 {
			return fmt.Errorf("security.session_secret (RUNWAY_SECURITY_SESSION_SECRET) must be at least 32 characters in production")
		}
		if strings.TrimSpace(c.Security.EncryptionKey) == "" {
			return fmt.Errorf("security.encryption_key (RUNWAY_SECURITY_ENCRYPTION_KEY) is required in production")
		}
	} else {
		if len(strings.TrimSpace(c.Security.SessionSecret)) < 32 {
			c.Security.SessionSecret = randomBase64(32)
			c.Security.EphemeralKeysInDev = true
		}
		if strings.TrimSpace(c.Security.EncryptionKey) == "" {
			c.Security.EncryptionKey = randomBase64(32)
			c.Security.EphemeralKeysInDev = true
		}
	}

	switch c.Moderation.DefaultAction {
	case "allow", "require_approval", "deny":
	default:
		return fmt.Errorf("invalid moderation.default_action %q", c.Moderation.DefaultAction)
	}

	switch c.Orchestrator.Provider {
	case "simulated", "docker", "agent":
	default:
		return fmt.Errorf("invalid orchestrator.provider %q", c.Orchestrator.Provider)
	}

	if c.Orchestrator.WorkerCount < 1 {
		c.Orchestrator.WorkerCount = 1
	}
	if c.Orchestrator.QueueSize < 8 {
		c.Orchestrator.QueueSize = 8
	}

	return nil
}

// IsProduction returns true when running in production mode.
func (c *Config) IsProduction() bool {
	return c.App.Environment == "production"
}

// Address returns host:port string for Fiber Listen.
func (c *Config) Address() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

func randomBase64(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Errorf("crypto/rand failed: %w", err))
	}
	return base64.StdEncoding.EncodeToString(buf)
}
