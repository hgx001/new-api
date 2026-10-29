// Package tencentmps_setting 保存「腾讯云 MPS 视频增强」所需的接入配置。
//
// 为什么单独一个包：MPS 是**第二跳服务**（官网生成 480P → MPS 增强 → 720P），
// 它的密钥 / COS 桶与「模型定价」是两回事，不该混进 ratio_setting。
// 而且这份配置**含明文密钥**，只允许通过 option 表下发，绝不能进日志或前端。
//
// 数据流：
//
//	官方生成成片 URL（OSS 直链）
//	  → MPS ProcessMedia（Type=URL 直链输入，无需中转下载）
//	  → 产物落 COS（OutputDir 下）
//	  → 本站按需签临时链接，客户端 302 直连 CDN 取字节
package tencentmps_setting

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// OptionKey 是这份配置在 options 表里的键名。
const OptionKey = "TencentMPSSetting"

// Config 是 MPS 接入配置。
type Config struct {
	// Enabled 是否启用后处理链路。
	// 关掉后，wan3.0-720p-官网 会直接回落成「480P 原片」，任务不会失败。
	Enabled bool `json:"enabled"`
	// SecretId 腾讯云 API 密钥 ID。
	SecretId string `json:"secret_id"`
	// SecretKey 腾讯云 API 密钥。⚠️ 明文存储，只在 option 表里，不要外泄。
	SecretKey string `json:"secret_key"`
	// Region 腾讯云地域，如 ap-guangzhou。
	Region string `json:"region"`
	// Bucket COS 桶名（MPS 产物落这里）。
	Bucket string `json:"bucket"`
	// OutputDir MPS 输出目录，必须以 / 开头和结尾，如 /mps_api_test/。
	//
	// ⚠️ 只能传**单层**目录：多层会被服务端当前缀处理，拼出
	// /C:/Users/.../PortableGit/mps_api_test/xxx/ 这种畸形 key
	// （文件仍在 COS，但 key 不可用）。这是 2026-10-29 实测踩到的。
	OutputDir string `json:"output_dir"`
	// SessionIdPrefix MPS 会话 ID 前缀，便于在腾讯云控制台里按前缀找任务。
	SessionIdPrefix string `json:"session_id_prefix"`
	// SuperResolutionType 超分算法档位：lq（默认）/ hq。
	// hq 细节保留更好（2026-10-29 实测 VMAF 97.09 vs lq 更低）。
	SuperResolutionType string `json:"super_resolution_type"`
	// OutputBitrateKbps 输出码率（kbps）。0 表示用默认 5000。
	OutputBitrateKbps int `json:"output_bitrate_kbps"`
	// OutputWidth/OutputHeight 输出分辨率。0 表示按目标档位自动推算（2 倍）。
	// ⚠️ 语义是 **(长边, 短边)**，不是宽×高 —— 与 MPS 的
	//	ResolutionAdaptive=open 一致（Width 代表长边、Height 表示短边）。
	//	填错会把竖屏片压扁，两个值都 > 0 时才生效。
	OutputWidth  int `json:"output_width"`
	OutputHeight int `json:"output_height"`
}

// defaultConfig 返回默认配置。
//
// ⚠️ 默认 Enabled=false：**没配 MPS 就不做后处理**，行为与改造前完全一致。
func defaultConfig() Config {
	return Config{
		Enabled:             false,
		Region:              "ap-guangzhou",
		OutputDir:           "/mps_api_test/",
		SessionIdPrefix:     "heibaidao",
		SuperResolutionType: "hq",
		OutputBitrateKbps:   5000,
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
// 只覆盖提交里带了的字段：管理员改「加价」类小项时不该把密钥清空。
// 这里用一个指针字段的中间结构做「部分更新」。
func UpdateFromJSONString(jsonStr string) error {
	var parsed Config
	if err := common.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	// 密钥类字段：提交为空视为「不改」，避免误清空导致链路突然失效。
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
	if parsed.SessionIdPrefix == "" {
		parsed.SessionIdPrefix = current.SessionIdPrefix
	}
	if parsed.SuperResolutionType == "" {
		parsed.SuperResolutionType = current.SuperResolutionType
	}
	if parsed.OutputBitrateKbps <= 0 {
		parsed.OutputBitrateKbps = current.OutputBitrateKbps
	}
	current = parsed
	return nil
}

// IsConfigured 返回是否具备可用的接入配置。
//
// ⚠️ 缺任何一项都算「不可用」——宁可不做后处理（用户拿 480P 原片），
// 也不要提交一个注定失败的 MPS 任务。
func IsConfigured() bool {
	cfg := Get()
	return cfg.Enabled &&
		cfg.SecretId != "" &&
		cfg.SecretKey != "" &&
		cfg.Region != "" &&
		cfg.Bucket != "" &&
		cfg.OutputDir != ""
}
