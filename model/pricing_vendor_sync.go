package model

import (
	"github.com/QuantumNous/new-api/common"
)

// SyncModelVendors 为「从未归类」的模型补上供应商。
//
// 背景（2026-11-22）：`initDefaultVendorMapping` 只对**新建**的模型条目归类，
// 已经在 models 表里登记过的模型（后台「模型管理」手工建的那些元数据行）
// 一直是 vendor_id 为空 —— 模型广场上这些视频/图片模型就显示成「无供应商」。
//
// 这里只处理 vendor_id 为空/0 的行：**已归类的一律不动**，管理员手工指定的
// 供应商不会被自动规则覆盖。
//
// 幂等：重复执行不会重复写；没有规则命中的模型保持未归类（不猜）。
func SyncModelVendors() (int, error) {
	var models []*Model
	if err := DB.Where("deleted_at IS NULL AND (vendor_id IS NULL OR vendor_id = 0)").
		Find(&models).Error; err != nil {
		return 0, err
	}
	if len(models) == 0 {
		return 0, nil
	}

	var vendors []Vendor
	if err := DB.Find(&vendors).Error; err != nil {
		return 0, err
	}
	vendorMap := make(map[int]*Vendor, len(vendors))
	for i := range vendors {
		vendorMap[vendors[i].Id] = &vendors[i]
	}

	updated := 0
	for _, m := range models {
		vendorName, ok := classifyModelVendor(m.ModelName)
		if !ok {
			continue
		}
		vendorID := getOrCreateVendor(vendorName, vendorMap)
		if vendorID == 0 {
			continue
		}
		if err := DB.Model(&Model{}).Where("id = ?", m.Id).
			Update("vendor_id", vendorID).Error; err != nil {
			common.SysError("自动归类供应商失败: " + m.ModelName + ": " + err.Error())
			continue
		}
		m.VendorID = vendorID
		updated++
	}
	return updated, nil
}

// ReclassifyModelVendors 按规则表**强制**重写所有能命中规则的模型供应商。
//
// 与 SyncModelVendors 的区别：这里连「已归类」的也会改。
// 只在明确要纠正历史误分类时用（例如手工分错了厂商）。
// 没有规则命中的模型保持原样（不猜、不清空）。
func ReclassifyModelVendors() (int, error) {
	var models []*Model
	if err := DB.Where("deleted_at IS NULL").Find(&models).Error; err != nil {
		return 0, err
	}

	var vendors []Vendor
	if err := DB.Find(&vendors).Error; err != nil {
		return 0, err
	}
	vendorMap := make(map[int]*Vendor, len(vendors))
	for i := range vendors {
		vendorMap[vendors[i].Id] = &vendors[i]
	}

	updated := 0
	for _, m := range models {
		vendorName, ok := classifyModelVendor(m.ModelName)
		if !ok {
			continue
		}
		vendorID := getOrCreateVendor(vendorName, vendorMap)
		if vendorID == 0 || vendorID == m.VendorID {
			continue
		}
		if err := DB.Model(&Model{}).Where("id = ?", m.Id).
			Update("vendor_id", vendorID).Error; err != nil {
			common.SysError("重写供应商失败: " + m.ModelName + ": " + err.Error())
			continue
		}
		m.VendorID = vendorID
		updated++
	}
	return updated, nil
}
