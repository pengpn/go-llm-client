// Package config 负责加载和验证应用配置。
//
// 加载优先级（高 → 低）：
//  1. 环境变量（适合生产/CI）
//  2. .env 文件（适合本地开发）
//  3. config.yaml（非敏感默认值）
//  4. 代码内置默认值
//
// 设计原则：
//   - 敏感信息（API Key）只走环境变量，不写入文件
//   - config.yaml 只存非敏感配置（模型参数、超时等）
//   - 启动时 Validate() 快速失败，避免运行时才发现配置缺失
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// Config 是整个应用的配置根结构。
type Config struct {
	LLM     LLMConfig     `yaml:"llm"`
	Session SessionConfig `yaml:"session"`
	Log     LogConfig     `yaml:"log"`
}

// LLMConfig 是 LLM Client 的配置。
type LLMConfig struct {
	Provider    string        `yaml:"provider"`     // openai / zhipu / tongyi / ollama
	BaseURL     string        `yaml:"base_url"`     // 自定义端点（可选）
	APIKey      string        `yaml:"-"`            // 从环境变量读取，不写入 yaml
	Model       string        `yaml:"model"`
	Temperature float64       `yaml:"temperature"`
	MaxTokens   int           `yaml:"max_tokens"`
	Timeout     time.Duration `yaml:"timeout"`
	MaxRetries  int           `yaml:"max_retries"`
	MaxConcurrent int         `yaml:"max_concurrent"`
}

// SessionConfig 是会话管理的配置。
type SessionConfig struct {
	TTL          time.Duration `yaml:"ttl"`
	MaxTurns     int           `yaml:"max_turns"`     // ByTurns 截断策略
	MaxTokens    int           `yaml:"max_tokens"`    // ByTokenEstimate 截断策略
	TruncateMode string        `yaml:"truncate_mode"` // "turns" 或 "tokens"
}

// LogConfig 是日志配置。
type LogConfig struct {
	Level  string `yaml:"level"`  // debug / info / warn / error
	Format string `yaml:"format"` // text / json
}

// Load 加载配置，按优先级合并所有来源。
// configPath 是 YAML 文件路径，不存在时跳过（不报错）。
func Load(configPath string) (*Config, error) {
	// 第一步：加载 .env 文件（忽略不存在的情况）
	// godotenv 只设置当前未设置的变量，不会覆盖已有环境变量
	_ = godotenv.Load(".env")

	// 第二步：从 YAML 文件加载基础配置
	cfg := defaults()
	if err := loadYAML(configPath, cfg); err != nil {
		return nil, err
	}

	// 第三步：环境变量覆盖（敏感信息只从这里读）
	applyEnv(cfg)

	// 第四步：验证必填项
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// defaults 返回内置默认配置。
func defaults() *Config {
	return &Config{
		LLM: LLMConfig{
			Provider:      "openai",
			Model:         "gpt-4o-mini",
			Temperature:   0.7,
			MaxTokens:     2048,
			Timeout:       60 * time.Second,
			MaxRetries:    3,
			MaxConcurrent: 10,
		},
		Session: SessionConfig{
			TTL:          30 * time.Minute,
			MaxTurns:     20,
			MaxTokens:    4000,
			TruncateMode: "turns",
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

// loadYAML 从文件加载 YAML 配置，合并到 cfg（文件值覆盖默认值）。
// 文件不存在时直接返回，不报错。
func loadYAML(path string, cfg *Config) error {
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // 文件不存在是正常情况
	}
	if err != nil {
		return fmt.Errorf("读取配置文件 %s 失败: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("解析配置文件 %s 失败: %w", path, err)
	}

	return nil
}

// applyEnv 从环境变量中读取配置，覆盖文件/默认值。
// 只有非空的环境变量才会覆盖。
func applyEnv(cfg *Config) {
	// API Key — 各 Provider 的环境变量名
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
	}
	if v := os.Getenv("ZHIPU_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
	}
	if v := os.Getenv("TONGYI_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
	}
	// 通用覆盖（优先级最高）
	if v := os.Getenv("LLM_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
	}

	// 其他可通过环境变量覆盖的字段
	if v := os.Getenv("LLM_MODEL"); v != "" {
		cfg.LLM.Model = v
	}
	if v := os.Getenv("LLM_BASE_URL"); v != "" {
		cfg.LLM.BaseURL = v
	}
	if v := os.Getenv("LLM_PROVIDER"); v != "" {
		cfg.LLM.Provider = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
}

// Validate 校验必填项，启动时快速失败。
func (c *Config) Validate() error {
	// Ollama 本地运行不需要 API Key
	if c.LLM.Provider != "ollama" && c.LLM.APIKey == "" {
		return fmt.Errorf(
			"缺少 API Key：请设置环境变量 OPENAI_API_KEY / ZHIPU_API_KEY / TONGYI_API_KEY / LLM_API_KEY",
		)
	}

	if c.LLM.Model == "" {
		return fmt.Errorf("LLM 模型名不能为空（llm.model）")
	}

	validModes := map[string]bool{"turns": true, "tokens": true}
	if !validModes[c.Session.TruncateMode] {
		return fmt.Errorf("无效的截断模式 %q，可选值：turns / tokens", c.Session.TruncateMode)
	}

	return nil
}
