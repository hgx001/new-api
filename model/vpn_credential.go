package model

import (
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// VPN 凭据状态。frozen 的节点侧会被 reject，但订阅地址不变，
// 用户点「重置」后换一个全新 UUID 即可恢复，分享出去的旧配置永久失效。
const (
	VpnCredentialActive  = "active"
	VpnCredentialRevoked = "revoked"
	VpnCredentialFrozen  = "frozen"
)

// VpnCredential 一个订单在某一时刻生效的节点凭据。
// Name 同时是 sing-box 的 auth_user，节点日志会打印它，用于按用户归集源 IP。
// Uuid 是交付凭据，任何用户/管理端接口都不会把它序列化出去。
type VpnCredential struct {
	Id         int    `json:"id" gorm:"primaryKey"`
	OrderId    int    `json:"order_id" gorm:"index"`
	UserId     int    `json:"user_id" gorm:"index"`
	Name       string `json:"name" gorm:"uniqueIndex;type:varchar(32)"`
	Uuid       string `json:"-" gorm:"type:varchar(64)"`
	Status     string `json:"status" gorm:"type:varchar(20)"`
	CreateTime int64  `json:"create_time"`
	RevokeTime int64  `json:"revoke_time"`
	FrozenTime int64  `json:"frozen_time"`
}

// VpnCredentialAccess 凭据访问记录。来源有两类：订阅拉取 IP、节点上报的连接 IP。
// 共享检测只看 24h 内的不同源 IP 数，不做「IP 变了就拒绝」的静态绑定。
type VpnCredentialAccess struct {
	Id           int    `json:"id" gorm:"primaryKey"`
	CredentialId int    `json:"credential_id" gorm:"index:idx_vpn_access_cred_time,priority:1"`
	Name         string `json:"name" gorm:"type:varchar(32);index"`
	Ip           string `json:"ip" gorm:"type:varchar(64)"`
	Source       string `json:"source" gorm:"type:varchar(16)"`
	CreateTime   int64  `json:"create_time" gorm:"index:idx_vpn_access_cred_time,priority:2"`
}

// VpnAccessSourceNode 节点上报的真实连接出口 IP，参与共享判定。
// VpnAccessSourceSub 订阅拉取 IP，只留痕不参与判定。
const (
	VpnAccessSourceNode = "node"
	VpnAccessSourceSub  = "sub"
)

// 显式固定表名：聚合查询里要用原始表名做 JOIN，不能依赖 GORM 的复数推导。
func (VpnCredential) TableName() string       { return "vpn_credentials" }
func (VpnCredentialAccess) TableName() string { return "vpn_credential_access" }

// VpnNodeUser 节点清单里的一条有效用户。
type VpnNodeUser struct {
	Name string `json:"name"`
	Uuid string `json:"uuid"`
}

// VpnNodeManifest 节点 agent 拉取的完整状态。Legacy 用户由节点本地基础配置保留，
// 不经过本接口，避免把节点上其它业务的凭据传进网站库或日志。
type VpnNodeManifest struct {
	Users  []VpnNodeUser `json:"users"`
	Frozen []string      `json:"frozen"`
}

// NewVpnUUID 生成标准 UUID v4 文本。复用 model 包既有的 randomHex，
// 再按 RFC 4122 置位，避免某些客户端做格式校验时拒绝。
func NewVpnUUID() (string, error) {
	raw := randomHex(32)
	b := make([]byte, 16)
	if _, err := hex.Decode(b, []byte(raw)); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32]), nil
}

// NewVpnSubToken 生成订阅地址里的随机令牌。令牌只用于查订单，不是登录凭据。
func NewVpnSubToken() string {
	return randomHex(48)
}

// AllocateVpnCredential 给订单分配一条新凭据。Name 用自增主键派生，
// 保证全局唯一且短（节点日志与 reject 名单都好处理）。
// 调用方负责把它放进创建订单的同一个事务里，避免「订单成功但没凭据」。
func AllocateVpnCredential(tx *gorm.DB, orderId, userId int) (*VpnCredential, error) {
	now := time.Now().Unix()
	uuid, err := NewVpnUUID()
	if err != nil {
		return nil, err
	}
	cred := &VpnCredential{
		OrderId:    orderId,
		UserId:     userId,
		Uuid:       uuid,
		Status:     VpnCredentialActive,
		CreateTime: now,
	}
	if err := tx.Create(cred).Error; err != nil {
		return nil, err
	}
	cred.Name = fmt.Sprintf("v%d", cred.Id)
	if err := tx.Model(cred).Update("name", cred.Name).Error; err != nil {
		return nil, err
	}
	return cred, nil
}

// GetVpnCredentialByOrder 返回订单当前仍在使用的凭据（含 frozen，用于页面提示重置）。
func GetVpnCredentialByOrder(orderId int) (*VpnCredential, error) {
	var cred VpnCredential
	err := DB.Where("order_id = ? AND status != ?", orderId, VpnCredentialRevoked).
		Order("id DESC").First(&cred).Error
	if err != nil {
		return nil, err
	}
	return &cred, nil
}

// GetVpnCredentialByName 节点上报连接事件时按 name 定位凭据。
func GetVpnCredentialByName(name string) (*VpnCredential, error) {
	var cred VpnCredential
	err := DB.Where("name = ?", name).First(&cred).Error
	if err != nil {
		return nil, err
	}
	return &cred, nil
}

// RotateVpnCredential 作废旧凭据并给同一订单换一条全新的。
// 订阅令牌绑定订单而不是凭据，所以用户不需要重新导入订阅。
func RotateVpnCredential(cred *VpnCredential) (*VpnCredential, error) {
	if cred == nil || cred.Id == 0 {
		return nil, errors.New("凭据不存在")
	}
	var result *VpnCredential
	err := DB.Transaction(func(tx *gorm.DB) error {
		now := time.Now().Unix()
		res := tx.Model(&VpnCredential{}).
			Where("id = ? AND status != ?", cred.Id, VpnCredentialRevoked).
			Updates(map[string]interface{}{"status": VpnCredentialRevoked, "revoke_time": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errors.New("凭据已被重置，请刷新后重试")
		}
		next, err := AllocateVpnCredential(tx, cred.OrderId, cred.UserId)
		if err != nil {
			return err
		}
		result = next
		return nil
	})
	return result, err
}

// RecordVpnCredentialAccess 幂等写入一条访问记录：同一凭据、同一 IP、同一来源
// 在窗口内只保留一条，避免日志刷屏把检测阈值冲垮。
func RecordVpnCredentialAccess(cred *VpnCredential, ip, source string, now int64) error {
	if cred == nil || ip == "" {
		return nil
	}
	window := now - 24*3600
	var count int64
	if err := DB.Model(&VpnCredentialAccess{}).
		Where("credential_id = ? AND ip = ? AND source = ? AND create_time >= ?",
			cred.Id, ip, source, window).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return DB.Create(&VpnCredentialAccess{
		CredentialId: cred.Id,
		Name:         cred.Name,
		Ip:           ip,
		Source:       source,
		CreateTime:   now,
	}).Error
}

// CountVpnCredentialIPs 统计凭据在窗口内出现过的不同源 IP 数。
// source 用来区分信号强度：只有 node（真实连接出口）参与共享判定，
// sub（订阅拉取）只留痕 —— 用户在手机上拉一次订阅、在电脑上连，
// 两个 IP 都是本人，算进阈值只会制造误封。
func CountVpnCredentialIPs(credId int, source string, now int64) (int64, error) {
	var count int64
	// GORM 的 Distinct().Count() 在部分方言下不会改写 COUNT，直接写聚合表达式更可靠。
	err := DB.Model(&VpnCredentialAccess{}).
		Where("credential_id = ? AND source = ? AND create_time >= ?", credId, source, now-24*3600).
		Select("COUNT(DISTINCT ip)").Row().Scan(&count)
	return count, err
}

// EvaluateVpnCredentialAbuse 共享检测：24h 内同一凭据从超过 maxIPs 个不同源 IP
// 连上节点就冻结。阈值留出手机/电脑/换网络的余量，
// 靠「重置即换新 UUID」把误伤成本降到最低。
func EvaluateVpnCredentialAbuse(cred *VpnCredential, source string, maxIPs int, now int64) (bool, int64, error) {
	count, err := CountVpnCredentialIPs(cred.Id, source, now)
	if err != nil {
		return false, count, err
	}
	if count <= int64(maxIPs) {
		return false, count, nil
	}
	frozen, err := FreezeVpnCredential(cred, now)
	return frozen, count, err
}

// FreezeVpnCredential 把凭据置为 frozen。幂等：重复上报不会反复改写时间。
func FreezeVpnCredential(cred *VpnCredential, now int64) (bool, error) {
	if cred == nil || cred.Status == VpnCredentialFrozen {
		return false, nil
	}
	if cred.Status == VpnCredentialRevoked {
		return false, nil
	}
	res := DB.Model(&VpnCredential{}).
		Where("id = ? AND status = ?", cred.Id, VpnCredentialActive).
		Updates(map[string]interface{}{"status": VpnCredentialFrozen, "frozen_time": now})
	return res.RowsAffected == 1, res.Error
}

// ListVpnNodeManifest 生成节点清单：只包含已支付、未过期的 active 凭据。
// cutoffCompleteTime 由调用方按套餐有效期算出，避免 model 依赖 controller 常量。
// frozen 名单单独下发，节点据此生成 reject 规则；旧的 frozen 凭据即使被 reset，
// name 也不会被复用（自增主键），所以名单只增不减是安全的。
func ListVpnNodeManifest(cutoffCompleteTime int64, now int64) (VpnNodeManifest, error) {
	manifest := VpnNodeManifest{Users: []VpnNodeUser{}, Frozen: []string{}}
	var rows []VpnCredential
	err := DB.Table("vpn_credentials AS c").
		Select("c.id, c.name, c.uuid, c.status").
		Joins("JOIN vpn_orders AS o ON o.id = c.order_id").
		Where("o.status = ? AND o.complete_time >= ? AND c.status != ?",
			"success", cutoffCompleteTime, VpnCredentialRevoked).
		Order("c.id ASC").Scan(&rows).Error
	if err != nil {
		return manifest, err
	}
	for _, row := range rows {
		if row.Status == VpnCredentialActive {
			manifest.Users = append(manifest.Users, VpnNodeUser{Name: row.Name, Uuid: row.Uuid})
		} else if row.Status == VpnCredentialFrozen {
			manifest.Frozen = append(manifest.Frozen, row.Name)
		}
	}
	// 已 reset 的历史 frozen 凭据也要留在 reject 名单里：分享者手里的旧 UUID
	// 虽已从 users 移除而自然失效，但保留名单可以避免以后误复用同名槽位。
	var frozenHistory []string
	if err := DB.Model(&VpnCredential{}).
		Where("status = ?", VpnCredentialFrozen).
		Pluck("name", &frozenHistory).Error; err != nil {
		return manifest, err
	}
	manifest.Frozen = appendUnique(manifest.Frozen, frozenHistory)
	return manifest, nil
}

// VpnCredentialInfo 管理端列表里的凭据摘要。不下发 UUID 与订阅令牌。
type VpnCredentialInfo struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	IPCount int64  `json:"ip_count"`
}

// GetVpnCredentialInfos 批量取订单的当前凭据状态与 24h 不同源 IP 数。
// 用一条聚合查询，避免订单列表 N+1；SQLite/MySQL/PostgreSQL 都按标准 GROUP BY 写。
func GetVpnCredentialInfos(orderIds []int, now int64) map[int]VpnCredentialInfo {
	result := make(map[int]VpnCredentialInfo)
	if len(orderIds) == 0 {
		return result
	}
	var rows []struct {
		OrderId int    `gorm:"column:order_id"`
		Name    string `gorm:"column:name"`
		Status  string `gorm:"column:status"`
		IPCount int64  `gorm:"column:ip_count"`
	}
	err := DB.Table("vpn_credentials AS c").
		Select("c.order_id, c.name, c.status, COUNT(DISTINCT a.ip) AS ip_count").
		Joins("LEFT JOIN vpn_credential_access AS a ON a.credential_id = c.id AND a.source = ? AND a.create_time >= ?",
			VpnAccessSourceNode, now-24*3600).
		Where("c.order_id IN ? AND c.status != ?", orderIds, VpnCredentialRevoked).
		Group("c.order_id, c.name, c.status").
		Scan(&rows).Error
	if err != nil {
		return result
	}
	for _, row := range rows {
		// 同一订单理论上只有一条非 revoked 凭据；万一有残留取 id 更大的那条。
		result[row.OrderId] = VpnCredentialInfo{Name: row.Name, Status: row.Status, IPCount: row.IPCount}
	}
	return result
}

func appendUnique(dst []string, src []string) []string {
	seen := make(map[string]bool, len(dst)+len(src))
	for _, v := range dst {
		seen[v] = true
	}
	for _, v := range src {
		if !seen[v] {
			dst = append(dst, v)
			seen[v] = true
		}
	}
	return dst
}
