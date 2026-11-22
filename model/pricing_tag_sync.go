package model

import (
	"github.com/QuantumNous/new-api/common"
)

// SyncModelTags 按规则补齐 / 纠正模型标签。
//
// 背景（2026-11-22）：生产两台的 models 表里，17~24 个视频/图片模型**没有
// 标签**（模型广场的「模型标签」筛选搜不到它们），而 gpt-6-luna / gpt-6-sol /
// gpt-6.1-sol 这三个**对话**模型被挂成了 image。
//
// 行为（与 SyncModelVendors 分工）：
//   - 媒体模型（video / image）：标签必须是且只能是规则给出的那个；
//   - 文本模型：摘掉 image/video 这类媒体标签，**其它标签保留**
//     （管理员写的推理、工具、1M 之类不会被抹掉）；
//   - 规则判不了的模型：原样不动，不猜。
func SyncModelTags() (int, error) {
	var models []*Model
	if err := DB.Where("deleted_at IS NULL").Find(&models).Error; err != nil {
		return 0, err
	}

	updated := 0
	for _, m := range models {
		newTags, changed := applyModelTag(m.Tags, m.ModelName)
		if !changed || newTags == m.Tags {
			continue
		}
		if err := DB.Model(&Model{}).Where("id = ?", m.Id).
			Update("tags", newTags).Error; err != nil {
			common.SysError("同步模型标签失败: " + m.ModelName + ": " + err.Error())
			continue
		}
		m.Tags = newTags
		updated++
	}
	return updated, nil
}
