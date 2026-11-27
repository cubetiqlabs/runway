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
	Cache        CacheConfig        `mapstructure:"cache"`
	EventStore   EventStoreConfig   `mapstructure:"eventstore"`
	GitHub       GitHubConfig       `mapstructure:"github"`
	Security     SecurityConfig     `mapstructure:"security"`
	Admin        AdminConfig        `mapstructure:"admin"`
	Moderation   ModerationConfig   `mapstructure:"moderation"`
	Orchestrator OrchestratorConfig `mapstructure:"orchestrator"`
	Logging      LoggingConfig      `mapstructure:"logging"`
}

type CacheConfig struct {
	Driver       string        `mapstructure:"driver"` // valkey | redis | memory | disabled
	URL          string        `mapstructure:"url"`
	Addrs        []string      `mapstructure:"addrs"`
	Password     string        `mapstructure:"password"`
	DB           int           `mapstructure:"db"`
	KeyPrefix    string        `mapstructure:"key_prefix"`
	DefaultTTL   time.Duration `mapstructure:"default_ttl"`
	PoolSize     int           `mapstructure:"pool_size"`
	MinIdleConns int           `mapstructure:"min_idle_conns"`
	DialTimeout  time.Duration `mapstructure:"dial_timeout"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	TLSEnabled   bool          `mapstructure:"tls_enabled"`
	InstanceID   string        `mapstructure:"instance_id"`
}

type EventStoreConfig struct {
	Driver             string        `mapstructure:"driver"` // postgres | clickhouse | disabled
	PostgresURL        string        `mapstructure:"postgres_url"`
	ClickHouseURL      string        `mapstructure:"clickhouse_url"`
	ClickHouseDatabase string        `mapstructure:"clickhouse_database"`
	ClickHouseUsername string        `mapstructure:"clickhouse_username"`
	ClickHousePassword string        `mapstructure:"clickhouse_password"`
	BatchSize          int           `mapstructure:"batch_size"`
	FlushInterval      time.Duration `mapstructure:"flush_interval"`
	BufferSize         int           `mapstructure:"buffer_size"`
	RetentionDays      int           `mapstructure:"retention_days"`
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
	Provider                string        `mapstructure:"provider"` // simulated | docker | agent
	DockerSocket            string        `mapstructure:"docker_socket"`
	DefaultImage            string        `mapstructure:"default_image"`
	SandboxRuntime          string        `mapstructure:"sandbox_runtime"` // auto | gvisor | runsc | runc | none | <custom>
	RunnerIsolatedDinD      bool          `mapstructure:"runner_isolated_dind"`
	DinDImage               string        `mapstructure:"dind_image"`
	RunnerDockerHost        string        `mapstructure:"runner_docker_host"`
	RunnerNetwork           string        `mapstructure:"runner_network"`
	RunnerMountDockerSocket bool          `mapstructure:"runner_mount_docker_socket"`
	RunnerDockerGID         string        `mapstructure:"runner_docker_gid"`
	RunnerDNS               []string      `mapstructure:"runner_dns"`
	WorkerCount             int           `mapstructure:"worker_count"`
	QueueSize               int           `mapstructure:"queue_size"`
	ReconcileInterval       time.Duration `mapstructure:"reconcile_interval"`
	ProvisionTimeout        time.Duration `mapstructure:"provision_timeout"`
	StaleHeartbeatTTL       time.Duration `mapstructure:"stale_heartbeat_ttl"`
	CacheEnabled            bool          `mapstructure:"cache_enabled"`
	CacheMaxGB              int           `mapstructure:"cache_max_gb"`
	PrewarmImages           bool          `mapstructure:"prewarm_images"`
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

	v.SetDefault("cache.driver", "memory")
	v.SetDefault("cache.url", "")
	v.SetDefault("cache.addrs", []string{})
	v.SetDefault("cache.password", "")
	v.SetDefault("cache.db", 0)
	v.SetDefault("cache.key_prefix", "runway:")
	v.SetDefault("cache.default_ttl", "30s")
	v.SetDefault("cache.pool_size", 32)
	v.SetDefault("cache.min_idle_conns", 4)
	v.SetDefault("cache.dial_timeout", "5s")
	v.SetDefault("cache.read_timeout", "3s")
	v.SetDefault("cache.write_timeout", "3s")
	v.SetDefault("cache.tls_enabled", false)
	v.SetDefault("cache.instance_id", "")

	v.SetDefault("eventstore.driver", "postgres")
	v.SetDefault("eventstore.postgres_url", "")
	v.SetDefault("eventstore.clickhouse_url", "")
	v.SetDefault("eventstore.clickhouse_database", "runway")
	v.SetDefault("eventstore.clickhouse_username", "default")
	v.SetDefault("eventstore.clickhouse_password", "")
	v.SetDefault("eventstore.batch_size", 256)
	v.SetDefault("eventstore.flush_interval", "500ms")
	v.SetDefault("eventstore.buffer_size", 8192)
	v.SetDefault("eventstore.retention_days", 90)

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
	v.SetDefault("orchestrator.default_image", "ghcr.io/cubetiqlabs/runway-runner:latest")
	v.SetDefault("orchestrator.sandbox_runtime", "auto")
	v.SetDefault("orchestrator.runner_isolated_dind", false)
	v.SetDefault("orchestrator.dind_image", "docker:dind")
	v.SetDefault("orchestrator.runner_docker_host", "")
	v.SetDefault("orchestrator.runner_network", "")
	v.SetDefault("orchestrator.runner_mount_docker_socket", false)
	v.SetDefault("orchestrator.runner_docker_gid", "")
	v.SetDefault("orchestrator.runner_dns", []string{})
	v.SetDefault("orchestrator.worker_count", 4)
	v.SetDefault("orchestrator.queue_size", 128)
	v.SetDefault("orchestrator.reconcile_interval", "30s")
	v.SetDefault("orchestrator.provision_timeout", "3m")
	v.SetDefault("orchestrator.stale_heartbeat_ttl", "5m")
	v.SetDefault("orchestrator.cache_enabled", true)
	v.SetDefault("orchestrator.cache_max_gb", 20)
	v.SetDefault("orchestrator.prewarm_images", true)

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
		"cache.driver", "cache.url", "cache.addrs", "cache.password", "cache.db",
		"cache.key_prefix", "cache.default_ttl", "cache.pool_size", "cache.min_idle_conns",
		"cache.dial_timeout", "cache.read_timeout", "cache.write_timeout",
		"cache.tls_enabled", "cache.instance_id",
		"eventstore.driver", "eventstore.postgres_url", "eventstore.clickhouse_url",
		"eventstore.clickhouse_database", "eventstore.clickhouse_username", "eventstore.clickhouse_password",
		"eventstore.batch_size", "eventstore.flush_interval", "eventstore.buffer_size", "eventstore.retention_days",
		"github.app_id", "github.app_slug", "github.client_id",
		"github.webhook_secret", "github.private_key_pem", "github.private_key_path",
		"github.api_base_url",
		"security.session_secret", "security.encryption_key", "security.session_ttl",
		"security.cookie_secure", "security.cookie_same_site",
		"security.login_rate_max", "security.webhook_rate_max",
		"admin.email", "admin.password", "admin.name",
		"moderation.default_action",
		"orchestrator.provider", "orchestrator.docker_socket", "orchestrator.default_image",
		"orchestrator.sandbox_runtime", "orchestrator.runner_isolated_dind", "orchestrator.dind_image",
		"orchestrator.runner_docker_host", "orchestrator.runner_network",
		"orchestrator.runner_mount_docker_socket", "orchestrator.runner_docker_gid",
		"orchestrator.runner_dns",
		"orchestrator.worker_count", "orchestrator.queue_size",
		"orchestrator.reconcile_interval", "orchestrator.provision_timeout",
		"orchestrator.stale_heartbeat_ttl",
		"orchestrator.cache_enabled", "orchestrator.cache_max_gb", "orchestrator.prewarm_images",
		"logging.level", "logging.format",
	}
	for _, k := range keys {
		_ = v.BindEnv(k)
	}
}

func (c *Config) validateAndNormalize() error {
	if len(c.Orchestrator.RunnerDNS) > 0 {
		normalized := make([]string, 0, len(c.Orchestrator.RunnerDNS))
		for _, item := range c.Orchestrator.RunnerDNS {
			for _, part := range strings.Split(item, ",") {
				if s := strings.TrimSpace(part); s != "" {
					normalized = append(normalized, s)
				}
			}
		}
		c.Orchestrator.RunnerDNS = normalized
	}
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

	c.EventStore.Driver = strings.ToLower(strings.TrimSpace(c.EventStore.Driver))
	if c.EventStore.Driver == "" {
		c.EventStore.Driver = "postgres"
	}
	switch c.EventStore.Driver {
	case "postgres", "clickhouse", "disabled":
	default:
		return fmt.Errorf("invalid eventstore.driver %q: must be 'postgres', 'clickhouse', or 'disabled'", c.EventStore.Driver)
	}
	if c.EventStore.Driver == "clickhouse" && strings.TrimSpace(c.EventStore.ClickHouseURL) == "" {
		return fmt.Errorf("eventstore.clickhouse_url (RUNWAY_EVENTSTORE_CLICKHOUSE_URL) is required when eventstore.driver is 'clickhouse'")
	}
	if c.EventStore.BatchSize < 1 {
		c.EventStore.BatchSize = 256
	}
	if c.EventStore.BufferSize < 64 {
		c.EventStore.BufferSize = 8192
	}
	if c.EventStore.FlushInterval <= 0 {
		c.EventStore.FlushInterval = 500 * time.Millisecond
	}
	if c.EventStore.RetentionDays < 1 {
		c.EventStore.RetentionDays = 90
	}

	if len(c.Cache.Addrs) > 0 {
		normalizedAddrs := make([]string, 0, len(c.Cache.Addrs))
		for _, item := range c.Cache.Addrs {
			for _, part := range strings.Split(item, ",") {
				if s := strings.TrimSpace(part); s != "" {
					normalizedAddrs = append(normalizedAddrs, s)
				}
			}
		}
		c.Cache.Addrs = normalizedAddrs
	}
	c.Cache.Driver = strings.ToLower(strings.TrimSpace(c.Cache.Driver))
	if c.Cache.Driver == "" {
		if strings.TrimSpace(c.Cache.URL) != "" || len(c.Cache.Addrs) > 0 {
			c.Cache.Driver = "valkey"
		} else {
			c.Cache.Driver = "memory"
		}
	}
	switch c.Cache.Driver {
	case "valkey", "redis", "memory", "disabled":
	default:
		return fmt.Errorf("invalid cache.driver %q: must be 'valkey', 'redis', 'memory', or 'disabled'", c.Cache.Driver)
	}
	if (c.Cache.Driver == "valkey" || c.Cache.Driver == "redis") && strings.TrimSpace(c.Cache.URL) == "" && len(c.Cache.Addrs) == 0 {
		return fmt.Errorf("cache.url (RUNWAY_CACHE_URL) or cache.addrs is required when cache.driver is %q", c.Cache.Driver)
	}
	if strings.TrimSpace(c.Cache.KeyPrefix) == "" {
		c.Cache.KeyPrefix = "runway:"
	}
	if c.Cache.DefaultTTL <= 0 {
		c.Cache.DefaultTTL = 30 * time.Second
	}
	if c.Cache.PoolSize <= 0 {
		c.Cache.PoolSize = 32
	}
	if c.Cache.MinIdleConns < 0 {
		c.Cache.MinIdleConns = 4
	}
	if c.Cache.DialTimeout <= 0 {
		c.Cache.DialTimeout = 5 * time.Second
	}
	if c.Cache.ReadTimeout <= 0 {
		c.Cache.ReadTimeout = 3 * time.Second
	}
	if c.Cache.WriteTimeout <= 0 {
		c.Cache.WriteTimeout = 3 * time.Second
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
