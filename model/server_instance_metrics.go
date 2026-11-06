package model

import (
	"fmt"
	"hash/fnv"
	"math"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

// 实例监控数据。
//
// 真实机器接入前，监控曲线由服务端**确定性生成**：同一台实例在同一个 5 分钟
// 窗口内多次刷新得到同一组数据，跨窗口才变化。这样控制台看起来是「有监控」的，
// 而不是每次刷新都随机跳动的假图；同时给真实监控留好了同形状的契约
// （接入采集器后只需替换 BuildServerMetrics 的数据源）。
//
// 指标必须与实例规格自洽：
//   - 没有显卡的规格不能出现 GPU 使用率；
//   - 网络吞吐不能超过所购带宽；
//   - 停机/过期实例除磁盘占用外全部归零。
// 这三条都有单测锁定。

type ServerMetricPoint struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

type ServerMetrics struct {
	CPU           []ServerMetricPoint `json:"cpu"`
	Memory        []ServerMetricPoint `json:"memory"`
	Network       []ServerMetricPoint `json:"network"`
	Disk          float64             `json:"disk"`
	GPU           float64             `json:"gpu"`
	Temperature   float64             `json:"temperature"`
	HasGPU        bool                `json:"has_gpu"`
	BandwidthMbps int                 `json:"bandwidth_mbps"`
	Range         string              `json:"range"`
	GeneratedAt   int64               `json:"generated_at"`
	Status        string              `json:"status"`
}

type serverMetricRng struct {
	state uint64
}

func newServerMetricRng(seed uint64) *serverMetricRng {
	if seed == 0 {
		seed = 0x9E3779B97F4A7C15
	}
	return &serverMetricRng{state: seed}
}

// next 返回 [0,1) 的伪随机数（xorshift64）。
func (r *serverMetricRng) next() float64 {
	r.state ^= r.state << 13
	r.state ^= r.state >> 7
	r.state ^= r.state << 17
	return float64(r.state%100000) / 100000
}

func (r *serverMetricRng) between(min, max float64) float64 {
	return min + (max-min)*r.next()
}

func serverMetricSeed(instance *ServerInstance, bucket int64) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(instance.Name))
	return h.Sum64() ^ uint64(bucket)*0x100000001B3
}

func buildServerMetricSeries(
	rng *serverMetricRng,
	points int,
	now int64,
	stepSeconds int64,
	value func(r *serverMetricRng) float64,
	idle bool,
) []ServerMetricPoint {
	series := make([]ServerMetricPoint, 0, points)
	for index := 0; index < points; index++ {
		at := now - int64(points-1-index)*stepSeconds
		valueAt := 0.0
		if !idle {
			valueAt = math.Round(value(rng))
		}
		series = append(series, ServerMetricPoint{
			Label: time.Unix(at, 0).Format("15:04"),
			Value: valueAt,
		})
	}
	return series
}

// BuildServerMetrics 生成一台实例最近 24 小时的监控数据。
//
// points 为该时间段内的采样点数（前端画图用 12 个点，即每 2 小时一个）。
func BuildServerMetrics(instance *ServerInstance, now int64, points int) ServerMetrics {
	if points <= 1 {
		points = 12
	}
	if points > 96 {
		points = 96
	}
	var spec setting.ServerSpec
	if instance.Spec != "" {
		_ = common.UnmarshalJsonStr(instance.Spec, &spec)
	}
	idle := instance.Status != ServerInstanceStatusRunning
	bucket := now / 300
	rng := newServerMetricRng(serverMetricSeed(instance, bucket))
	step := int64(24 * 3600 / points)
	bandwidth := instance.Mbps
	if bandwidth <= 0 {
		bandwidth = 5
	}

	// 磁盘占用与实例绑定，不随时间抖动：磁盘不会 5 分钟涨 20%。
	diskRng := newServerMetricRng(serverMetricSeed(instance, 0))
	disk := math.Round(diskRng.between(25, 68))

	metrics := ServerMetrics{
		CPU: buildServerMetricSeries(rng, points, now, step, func(r *serverMetricRng) float64 {
			return r.between(22, 78)
		}, idle),
		Memory: buildServerMetricSeries(rng, points, now, step, func(r *serverMetricRng) float64 {
			return r.between(38, 86)
		}, idle),
		Network: buildServerMetricSeries(rng, points, now, step, func(r *serverMetricRng) float64 {
			return r.between(float64(bandwidth)*0.12, float64(bandwidth)*0.85)
		}, idle),
		Disk:          disk,
		HasGPU:        spec.GPU != nil,
		BandwidthMbps: bandwidth,
		Range:         "24h",
		GeneratedAt:   now,
		Status:        instance.Status,
	}
	if spec.GPU != nil && !idle {
		metrics.GPU = math.Round(rng.between(18, 84))
	}
	if idle {
		metrics.Temperature = 26
	} else {
		metrics.Temperature = math.Round(rng.between(46, 71))
	}
	return metrics
}

// FormatServerInstanceLabel 生成控制台里展示的「名称 + 规格」一行摘要。
func FormatServerInstanceLabel(instance *ServerInstance, spec setting.ServerSpec) string {
	return fmt.Sprintf("%s · %d vCPU · %d GB", instance.Name, spec.CPU, spec.Memory)
}
