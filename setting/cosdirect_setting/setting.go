// Package cosdirect_setting 保存「COS 直存转存」链路的接入配置。
//
// 与 tencentmps_setting（MPS 转码转存）的区别：
//
//	MPS 链路   ：上游 URL → MPS 转码 → COS，多一次转码（2026-11-12 实测
//	            会把 10.75Mbps 压到 1.81Mbps，画质不可逆损失）；
//	直存链路   ：上游 URL → 服务器拉片 → 内网 PUT 到 COS，字节级无损。
//
// 两者产物都落 COS、都用 cos-proxy:// 占位符，取片路径完全一致。
// 这份配置含明文密钥，只允许通过 option 表下发，不能进日志或前端。
package cosdirect_setting

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// OptionKey 是这份配置在 options 表里的键名。
const OptionKey = "CosDirectSetting"

// Config 是 COS 直存接入配置。
type Config struct {
	// Enabled 是否启用直存链路。关掉后成片透传上游原片，行为与未改造一致。
	Enabled bool `json:"enabled"`
	// SecretId 腾讯云 API 密钥 ID。
	SecretId string `json:"secret_id"`
	// SecretKey 腾讯云 API 密钥。⚠️ 明文存储，只在 option 表里，不要外泄。
	SecretKey string `json:"secret_key"`
	// Region COS 地域，如 ap-guangzhou。
	Region string `json:"region"`
	// Bucket COS 桶名（成片落这里，必须私有读写）。
	Bucket string `json:"bucket"`
	// OutputDir 成片输出目录，必须以 / 开头，如 /videos/。
	OutputDir string `json:"output_dir"`
	// MaxBytes 单个成片的大小上限。0 表示默认 512MB。
	// 保护桶与内存：上游异常返回超大响应时直接判转存失败、回落原片。
	MaxBytes int64 `json:"max_bytes"`
	// TimeoutSec 拉片+上传的整体超时秒数。0 表示默认 600。
	TimeoutSec int `json:"timeout_sec"`
}

// defaultMaxBytes 单个成片大小上限默认值（512MB，覆盖现有所有视频档位）。
const defaultMaxBytes int64 = 512 << 20

// defaultTimeoutSec 拉片+上传默认超时（老机实测转存级操作 40-160s，留足余量）。
const defaultTimeoutSec = 600

// defaultConfig 返回默认配置。
//
// ⚠️ 默认 Enabled=false：没配直存就不做转存，行为与改造前完全一致。
func defaultConfig() Config {
	return Config{
		Enabled:    false,
		Region:     "ap-guangzhou",
		OutputDir:  "/videos/",
		MaxBytes:   defaultMaxBytes,
		TimeoutSec: defaultTimeoutSec,
	}
}

var (
	mu      sync.RWMutex
	current = defaultConfig()
)

// Get 返回当前配置的深拷贝。
func Get() Config {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// MarshalJSONString 把当前配置序列化成 JSON（写回 option 用）。
func MarshalJSONString() string {
	mu.RLock()
	defer mu.RUnlock()
	encoded, err := common.Marshal(current)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// UpdateFromJSONString 用管理员提交的 JSON 覆盖配置。
//
// 只覆盖提交里带了的字段：管理员改「输出目录」类小项时不该把密钥清空。
// 密钥类字段提交为空视为「不改」。
func UpdateFromJSONString(jsonStr string) error {
	var parsed Config
	if err := common.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if parsed.SecretId == "" {
		parsed.SecretId = current.SecretId
	}
	if parsed.SecretKey == "" {
		parsed.SecretKey = current.SecretKey
	}
	if parsed.Region == "" {
		parsed.Region = current.Region
	}
	if parsed.Bucket == "" {
		parsed.Bucket = current.Bucket
	}
	if parsed.OutputDir == "" {
		parsed.OutputDir = current.OutputDir
	}
	if parsed.MaxBytes <= 0 {
		parsed.MaxBytes = current.MaxBytes
	}
	if parsed.TimeoutSec <= 0 {
		parsed.TimeoutSec = current.TimeoutSec
	}
	current = parsed
	return nil
}

// IsConfigured 返回是否具备可用的接入配置。
//
// ⚠️ 缺任何一项都算「不可用」——宁可不做转存（用户拿上游原片），
// 也不要提交一个注定失败的上传。
func IsConfigured() bool {
	cfg := Get()
	return cfg.Enabled &&
		cfg.SecretId != "" &&
		cfg.SecretKey != "" &&
		cfg.Region != "" &&
		cfg.Bucket != "" &&
		cfg.OutputDir != ""
}
