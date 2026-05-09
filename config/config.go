package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Config holds all application configuration.
type Config struct {
	Server  ServerConfig
	LLM     LLMConfig
	Queue   QueueConfig
	Paths   PathsConfig
	Logging LoggingConfig
	Bash    BashConfig
}

type ServerConfig struct {
	Host         string
	Port         int
	WebhookPath  string
	MaxBodyBytes int
}

type LLMConfig struct {
	Endpoint           string
	Model              string
	APIKey             string
	ContextTokens      int
	MaxTokens          int
	Timeout            time.Duration
	MaxToolIterations  int
	SummarizeThreshold float64
	SummarizeKeepRecent  int
	SystemPrompt       string
}

type QueueConfig struct {
	MaxDepth int
}

type PathsConfig struct {
	WorkingDir    string
	LogDir        string
	StateDir      string
	ChannelLogDir string
}

type LoggingConfig struct {
	Level             string
	LogToolCalls      bool
	LogAgentReasoning bool
	LogChannelEvents  bool
}

type BashConfig struct {
	Enabled   bool
	Timeout   time.Duration
	MaxOutput int
	Banned    []string
}

// Load reads an INI file and builds a Config with defaults applied.
// Returns a fatal error if required values (llm.endpoint, llm.model) are missing.
func Load(path string) (*Config, error) {
	data, err := ParseFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}

	// Server section
	cfg.Server.Host = strDefault(data, "server", "host", "127.0.0.1")
	cfg.Server.Port = intDefault(data, "server", "port", 8080)
	cfg.Server.WebhookPath = strDefault(data, "server", "webhook_path", "/webhook")
	cfg.Server.MaxBodyBytes = intDefault(data, "server", "max_body_bytes", 1048576)

	// LLM section
	cfg.LLM.Endpoint = strDefault(data, "llm", "endpoint", "")
	cfg.LLM.Model = strDefault(data, "llm", "model", "")
	cfg.LLM.APIKey = strDefault(data, "llm", "api_key", "")
	cfg.LLM.ContextTokens = intDefault(data, "llm", "context_tokens", 8192)
	cfg.LLM.MaxTokens = intDefault(data, "llm", "max_tokens", 4096)
	cfg.LLM.Timeout = time.Duration(intDefault(data, "llm", "timeout", 120)) * time.Second
	cfg.LLM.MaxToolIterations = intDefault(data, "llm", "max_tool_iterations", 20)
	cfg.LLM.SummarizeThreshold = floatDefault(data, "llm", "summarize_threshold", 0.70)
	cfg.LLM.SummarizeKeepRecent = intDefault(data, "llm", "summarize_keep_recent", 10)
	cfg.LLM.SystemPrompt = strDefault(data, "llm", "system_prompt", "You are a helpful assistant.")

	// Queue section
	cfg.Queue.MaxDepth = intDefault(data, "queue", "max_depth", 64)

	// Paths section
	cfg.Paths.WorkingDir = strDefault(data, "paths", "working_dir", "./work/")
	cfg.Paths.LogDir = strDefault(data, "paths", "log_dir", "./logs/")
	cfg.Paths.StateDir = strDefault(data, "paths", "state_dir", "./state/")
	cfg.Paths.ChannelLogDir = strDefault(data, "paths", "channel_log_dir", "")

	// Logging section
	cfg.Logging.Level = strDefault(data, "logging", "level", "info")
	cfg.Logging.LogToolCalls = boolDefault(data, "logging", "log_tool_calls", true)
	cfg.Logging.LogAgentReasoning = boolDefault(data, "logging", "log_agent_reasoning", true)
	cfg.Logging.LogChannelEvents = boolDefault(data, "logging", "log_channel_events", true)

	// Bash tool section
	cfg.Bash.Enabled = boolDefault(data, "tools.bash", "enabled", true)
	cfg.Bash.Timeout = time.Duration(intDefault(data, "tools.bash", "timeout", 60)) * time.Second
	cfg.Bash.MaxOutput = intDefault(data, "tools.bash", "max_output", 30720)
	cfg.Bash.Banned = strListDefault(data, "tools.bash", "banned", "curl,wget,ssh,scp,ssh-keygen,nc,telnet,sudo,su,doas,rm,dd,chmod,chown,apt,apt-get,apt-cache,yum,dnf,brew,pip,pip3,npm,yarn,npx,pkg,pkg_add,apk,aptitude,makepkg,paru,pacman,zypper,rpm,emerge,service,systemctl,systemd,firewall-ctd,iptables,ufw,netstat,ifconfig,ip,route,crontab,at,batch,chkconfig,fdisk,mkfs,mount,umount,parted,scp,rsync")

	return cfg, nil
}

// Validate checks that required fields are present. Returns an error for fatal config issues.
func (c *Config) Validate() error {
	var errors []string

	if c.LLM.Endpoint == "" {
		errors = append(errors, "llm.endpoint is required (fatal)")
	}
	if c.LLM.Model == "" {
		errors = append(errors, "llm.model is required (fatal)")
	}
	if c.LLM.ContextTokens <= 0 {
		errors = append(errors, "llm.context_tokens must be positive")
	}
	if c.LLM.MaxTokens <= 0 {
		errors = append(errors, "llm.max_tokens must be positive")
	}
	if c.LLM.Timeout <= 0 {
		errors = append(errors, "llm.timeout must be positive")
	}
	if c.LLM.MaxToolIterations <= 0 {
		errors = append(errors, "llm.max_tool_iterations must be positive")
	}
	if c.LLM.SummarizeThreshold <= 0 || c.LLM.SummarizeThreshold > 1 {
		errors = append(errors, "llm.summarize_threshold must be between 0 and 1")
	}
	if c.LLM.SummarizeKeepRecent < 0 {
		errors = append(errors, "llm.summarize_keep_recent must be non-negative")
	}
	if c.Queue.MaxDepth <= 0 {
		errors = append(errors, "queue.max_depth must be positive")
	}
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		errors = append(errors, "server.port must be between 1 and 65535")
	}
	if c.Server.MaxBodyBytes <= 0 {
		errors = append(errors, "server.max_body_bytes must be positive")
	}

	// Validate log level
	level := strings.ToLower(c.Logging.Level)
	switch level {
	case "debug", "info", "warn", "error":
	default:
		errors = append(errors, fmt.Sprintf("logging.level %q is invalid (must be debug, info, warn, or error)", c.Logging.Level))
	}

	// Validate bash config
	if c.Bash.Timeout <= 0 {
		errors = append(errors, "tools.bash.timeout must be positive")
	}
	if c.Bash.MaxOutput <= 0 {
		errors = append(errors, "tools.bash.max_output must be positive")
	}

	if len(errors) > 0 {
		return fmt.Errorf("config validation failed:\n  - %s", strings.Join(errors, "\n  - "))
	}
	return nil
}

// strDefault returns the value for section/key, or defaultValue if missing.
func strDefault(data map[string]map[string]string, section, key, defaultValue string) string {
	if sec, ok := data[section]; ok {
		if val, ok := sec[key]; ok {
			return val
		}
	}
	return defaultValue
}

// intDefault parses an integer value, falling back to defaultValue on missing or invalid.
func intDefault(data map[string]map[string]string, section, key string, defaultValue int) int {
	if sec, ok := data[section]; ok {
		if raw, ok := sec[key]; ok {
			val, err := strconv.Atoi(strings.TrimSpace(raw))
			if err == nil {
				return val
			}
		}
	}
	return defaultValue
}

// boolDefault parses a boolean value, falling back to defaultValue on missing or invalid.
func boolDefault(data map[string]map[string]string, section, key string, defaultValue bool) bool {
	if sec, ok := data[section]; ok {
		if raw, ok := sec[key]; ok {
			val, err := strconv.ParseBool(strings.TrimSpace(raw))
			if err == nil {
				return val
			}
		}
	}
	return defaultValue
}

// strListDefault returns a comma-separated list of trimmed, lowercased strings,
// falling back to the defaultValue split the same way.
func strListDefault(data map[string]map[string]string, section, key, defaultValue string) []string {
	raw := strDefault(data, section, key, defaultValue)
	var result []string
	for _, item := range strings.Split(raw, ",") {
		s := strings.ToLower(strings.TrimSpace(item))
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

// floatDefault parses a float64 value, falling back to defaultValue on missing or invalid.
func floatDefault(data map[string]map[string]string, section, key string, defaultValue float64) float64 {
	if sec, ok := data[section]; ok {
		if raw, ok := sec[key]; ok {
			val, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
			if err == nil {
				return val
			}
		}
	}
	return defaultValue
}
