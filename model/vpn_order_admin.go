package model

import (
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// VpnOrderAdminQuery 管理端订单列表查询条件。Status 为空表示不过滤。
type VpnOrderAdminQuery struct {
	Page     int
	PageSize int
	Keyword  string // 订单号 / 用户 ID / 用户名
	Status   string
}

// VpnProviderStat 单个收款通道的已支付汇总。
type VpnProviderStat struct {
	Provider   string `json:"provider"`
	PaidCount  int64  `json:"paid_count"`
	PaidAmount string `json:"paid_amount"`
}

// VpnOrderStats 管理端收入汇总。金额统一为「分」精度的十进制字符串，
// 避免前端用 float 再做一次格式化导致展示与库内金额不一致。
type VpnOrderStats struct {
	PaidCount       int64             `json:"paid_count"`
	PaidAmount      string            `json:"paid_amount"`
	PendingCount    int64             `json:"pending_count"`
	TodayPaidCount  int64             `json:"today_paid_count"`
	TodayPaidAmount string            `json:"today_paid_amount"`
	ByProvider      []VpnProviderStat `json:"by_provider"`
}

// IsValidVpnOrderStatus 状态参数白名单：空串表示全部，其余必须是订单的真实终态。
func IsValidVpnOrderStatus(status string) bool {
	switch status {
	case "", common.TopUpStatusPending, common.TopUpStatusSuccess,
		common.TopUpStatusFailed, common.TopUpStatusExpired:
		return true
	default:
		return false
	}
}

// vpnOrderFilteredQuery 只复用过滤条件，不复用 *gorm.DB：
// Count 与 Find 各建一条链，避免 GORM 在 Count 后把 LIMIT/SELECT 残留到下一次查询。
func vpnOrderFilteredQuery(q VpnOrderAdminQuery) *gorm.DB {
	db := DB.Model(&VpnOrder{})
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	kw := strings.TrimSpace(q.Keyword)
	if kw == "" {
		return db
	}
	like := "%" + kw + "%"
	// 用 Table 而不是 Model(&User{})：避免把 User 模型的软删除/钩子条件带进子查询。
	usernameIDs := DB.Table("users").Select("id").Where("username LIKE ?", like)
	numericID := int64(0) // 用户 ID 从 1 开始，0 不会误匹配
	if parsed, err := strconv.ParseInt(kw, 10, 64); err == nil {
		numericID = parsed
	}
	return db.Where("(trade_no LIKE ? OR user_id IN (?) OR user_id = ?)",
		like, usernameIDs, numericID)
}

// SearchVpnOrders 管理端分页查询。按创建时间倒序，同一秒用 id 兜底保证稳定顺序。
func SearchVpnOrders(q VpnOrderAdminQuery) ([]VpnOrder, int64, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 {
		q.PageSize = common.ItemsPerPage
	}
	if q.PageSize > 100 {
		q.PageSize = 100
	}

	var total int64
	if err := vpnOrderFilteredQuery(q).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	orders := make([]VpnOrder, 0, q.PageSize)
	err := vpnOrderFilteredQuery(q).
		Order("create_time DESC").
		Order("id DESC").
		Offset((q.Page - 1) * q.PageSize).
		Limit(q.PageSize).
		Find(&orders).Error
	if err != nil {
		return nil, 0, err
	}
	return orders, total, nil
}

// GetVpnUsernames 批量取买家用户名。users 表异常时降级为空 map，
// 不能因为展示字段失败而让整张订单列表打不开。
func GetVpnUsernames(ids []int) map[int]string {
	result := make(map[int]string, len(ids))
	if len(ids) == 0 {
		return result
	}
	var rows []struct {
		ID       int    `gorm:"column:id"`
		Username string `gorm:"column:username"`
	}
	if err := DB.Table("users").Select("id, username").
		Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return result
	}
	for _, row := range rows {
		result[row.ID] = row.Username
	}
	return result
}

// sumVpnMoney 汇总指定条件下的已支付金额。SQLite 的 SUM 返回 float64、
// PostgreSQL/MySQL 返回十进制，统一交给 decimal.Decimal.Scan 处理。
func sumVpnMoney(db *gorm.DB) (string, error) {
	var sum decimal.Decimal
	if err := db.Select("COALESCE(SUM(money), 0)").Row().Scan(&sum); err != nil {
		return "", err
	}
	return sum.Round(2).StringFixed(2), nil
}

// GetVpnOrderStats 只读汇总：累计/当日已支付金额与笔数、待支付笔数、按通道分组。
// now 由调用方注入，便于用固定时间做确定性测试；当日边界按服务器本地时区。
func GetVpnOrderStats(now time.Time) (VpnOrderStats, error) {
	stats := VpnOrderStats{ByProvider: []VpnProviderStat{}}
	// 每条查询都重新建链：GORM 的 Where 之后会共享 statement，
	// 复用同一个 *gorm.DB 会把上一次的 Select/Group 累积到下一次查询。
	successQuery := func() *gorm.DB {
		return DB.Model(&VpnOrder{}).Where("status = ?", common.TopUpStatusSuccess)
	}
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	todaySuccessQuery := func() *gorm.DB {
		return DB.Model(&VpnOrder{}).
			Where("status = ? AND complete_time >= ?", common.TopUpStatusSuccess, todayStart)
	}

	if err := successQuery().Count(&stats.PaidCount).Error; err != nil {
		return stats, err
	}
	paidAmount, err := sumVpnMoney(successQuery())
	if err != nil {
		return stats, err
	}
	stats.PaidAmount = paidAmount

	if err := DB.Model(&VpnOrder{}).
		Where("status = ?", common.TopUpStatusPending).
		Count(&stats.PendingCount).Error; err != nil {
		return stats, err
	}

	if err := todaySuccessQuery().Count(&stats.TodayPaidCount).Error; err != nil {
		return stats, err
	}
	todayAmount, err := sumVpnMoney(todaySuccessQuery())
	if err != nil {
		return stats, err
	}
	stats.TodayPaidAmount = todayAmount

	var rows []struct {
		PaymentProvider string          `gorm:"column:payment_provider"`
		PaidCount       int64           `gorm:"column:paid_count"`
		PaidAmount      decimal.Decimal `gorm:"column:paid_amount"`
	}
	if err := successQuery().
		Select("payment_provider, COUNT(*) AS paid_count, COALESCE(SUM(money), 0) AS paid_amount").
		Group("payment_provider").
		Order("payment_provider").
		Scan(&rows).Error; err != nil {
		return stats, err
	}
	for _, row := range rows {
		provider := row.PaymentProvider
		if provider == "" {
			provider = "unknown"
		}
		stats.ByProvider = append(stats.ByProvider, VpnProviderStat{
			Provider:   provider,
			PaidCount:  row.PaidCount,
			PaidAmount: row.PaidAmount.Round(2).StringFixed(2),
		})
	}
	return stats, nil
}
