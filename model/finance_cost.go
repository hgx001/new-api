package model

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	ChannelCostStatusActive = "active"
	ChannelCostStatusVoided = "voided"
)

const (
	ChannelCostCategoryTopUp     = "topup"
	ChannelCostCategoryPrepay    = "api_prepay"
	ChannelCostCategoryBandwidth = "bandwidth"
	ChannelCostCategoryOther     = "other"
	ChannelCostCategoryRefund    = "refund"
)

var ErrChannelCostAmountInvalid = errors.New("invalid cost amount")

// ChannelCost 是运营手工录入的上游支出（充值 / 付款）。
//
// 两套口径共用这一张表：现金口径按 occurred_at 汇总全额，经营口径按 channel_id
// 归属到渠道。金额以「分」为单位的整数保存，避免浮点在多次汇总后漂移；
// ChannelId=0 表示无法归属到单一渠道的公共成本，汇总里单列，不摊进渠道毛利。
// 删除只置 status=voided，保留原行与审计链。
type ChannelCost struct {
	Id          int    `json:"id"`
	ChannelId   int    `json:"channel_id" gorm:"index:idx_finance_cost_channel"`
	AmountCents int64  `json:"amount_cents" gorm:"bigint"`
	OccurredAt  int64  `json:"occurred_at" gorm:"bigint;index:idx_finance_cost_time"`
	Category    string `json:"category" gorm:"type:varchar(32)"`
	ReferenceNo string `json:"reference_no" gorm:"type:varchar(64)"`
	Note        string `json:"note" gorm:"type:text"`
	Status      string `json:"status" gorm:"type:varchar(16);index:idx_finance_cost_status"`
	CreatedBy   int    `json:"created_by"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint"`
	ChannelName string `json:"channel_name,omitempty" gorm:"-"`
}

func IsValidChannelCostCategory(v string) bool {
	switch v {
	case ChannelCostCategoryTopUp, ChannelCostCategoryPrepay,
		ChannelCostCategoryBandwidth, ChannelCostCategoryOther, ChannelCostCategoryRefund:
		return true
	default:
		return false
	}
}

// ParseChannelCostAmount 把用户输入的人民币金额字符串转成「分」。
// 拒绝空值、零值、超过两位小数与明显越界的金额；退款/冲正用负数录入。
func ParseChannelCostAmount(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, ErrChannelCostAmountInvalid
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return 0, ErrChannelCostAmountInvalid
	}
	cents := d.Mul(decimal.NewFromInt(100))
	if !cents.Equal(cents.Truncate(0)) {
		return 0, ErrChannelCostAmountInvalid
	}
	v := cents.IntPart()
	if v == 0 || v > 100000000000 || v < -100000000000 {
		return 0, ErrChannelCostAmountInvalid
	}
	return v, nil
}

func CentsToDecimal(cents int64) decimal.Decimal {
	return decimal.NewFromInt(cents).Div(decimal.NewFromInt(100))
}

func CentsToAmountString(cents int64) string {
	return CentsToDecimal(cents).Round(2).StringFixed(2)
}

type ChannelOption struct {
	Id   int    `json:"id" gorm:"column:id"`
	Name string `json:"name" gorm:"column:name"`
}

// ListChannelOptions 供成本录入弹窗选择归属渠道，只取 id/name，不暴露 key 等敏感字段。
func ListChannelOptions() ([]ChannelOption, error) {
	options := make([]ChannelOption, 0)
	err := DB.Table("channels").Select("id, name").Order("id ASC").Scan(&options).Error
	return options, err
}

func ChannelExists(channelId int) bool {
	if channelId == 0 {
		return true
	}
	var count int64
	if err := DB.Table("channels").Where("id = ?", channelId).Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

func GetChannelCostById(id int) (*ChannelCost, error) {
	cost := &ChannelCost{}
	if err := DB.Where("id = ?", id).First(cost).Error; err != nil {
		return nil, err
	}
	return cost, nil
}

func CreateChannelCost(cost *ChannelCost) error {
	now := common.GetTimestamp()
	cost.Status = ChannelCostStatusActive
	cost.CreatedAt = now
	cost.UpdatedAt = now
	return DB.Create(cost).Error
}

// UpdateChannelCost 只允许改还有效的记录，fields 由调用方白名单构造。
func UpdateChannelCost(id int, fields map[string]interface{}) (bool, error) {
	fields["updated_at"] = common.GetTimestamp()
	result := DB.Model(&ChannelCost{}).
		Where("id = ? AND status = ?", id, ChannelCostStatusActive).
		Updates(fields)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func VoidChannelCost(id int) (bool, error) {
	result := DB.Model(&ChannelCost{}).
		Where("id = ? AND status = ?", id, ChannelCostStatusActive).
		Updates(map[string]interface{}{
			"status":     ChannelCostStatusVoided,
			"updated_at": common.GetTimestamp(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

type ChannelCostQuery struct {
	Page          int
	PageSize      int
	ChannelId     int // -1 表示全部
	Start         int64
	End           int64
	Keyword       string
	IncludeVoided bool
}

func channelCostFilteredQuery(q ChannelCostQuery) *gorm.DB {
	db := DB.Model(&ChannelCost{})
	if !q.IncludeVoided {
		db = db.Where("status = ?", ChannelCostStatusActive)
	}
	if q.ChannelId >= 0 {
		db = db.Where("channel_id = ?", q.ChannelId)
	}
	if q.Start > 0 {
		db = db.Where("occurred_at >= ?", q.Start)
	}
	if q.End > 0 {
		db = db.Where("occurred_at <= ?", q.End)
	}
	kw := strings.TrimSpace(q.Keyword)
	if kw != "" {
		like := "%" + kw + "%"
		// 不用 CAST(... AS CHAR)：PostgreSQL 没有 CHAR 目标类型，纯数字用参数化等值比较。
		if numericId, err := strconv.Atoi(kw); err == nil {
			db = db.Where("(reference_no LIKE ? OR note LIKE ? OR channel_id = ?)", like, like, numericId)
		} else {
			db = db.Where("(reference_no LIKE ? OR note LIKE ?)", like, like)
		}
	}
	return db
}

// SearchChannelCosts 返回分页明细、总数、以及筛选条件下有效金额的合计（分）。
// 合计不分页，供前端显示「本筛选合计」，避免翻页时金额对不上。
func SearchChannelCosts(q ChannelCostQuery) ([]ChannelCost, int64, int64, error) {
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
	if err := channelCostFilteredQuery(q).Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}

	items := make([]ChannelCost, 0, q.PageSize)
	if err := channelCostFilteredQuery(q).
		Order("occurred_at DESC").
		Order("id DESC").
		Offset((q.Page - 1) * q.PageSize).
		Limit(q.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, 0, err
	}

	var sum decimal.Decimal
	if err := channelCostFilteredQuery(q).
		Select("COALESCE(SUM(amount_cents), 0)").
		Row().Scan(&sum); err != nil {
		return nil, 0, 0, err
	}
	fillChannelCostNames(items)
	return items, total, sum.IntPart(), nil
}

// ListActiveChannelCosts 导出用：不分页，按发生时间正序。
func ListActiveChannelCosts(start, end int64) ([]ChannelCost, error) {
	items := make([]ChannelCost, 0)
	err := DB.Model(&ChannelCost{}).
		Where("status = ? AND occurred_at >= ? AND occurred_at <= ?", ChannelCostStatusActive, start, end).
		Order("occurred_at ASC").
		Order("id ASC").
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	fillChannelCostNames(items)
	return items, nil
}

// SumActiveChannelCostsByChannel 按渠道汇总区间内的有效成本（分）。
// SUM(bigint) 在三种数据库返回不同类型，统一交给 decimal.Decimal.Scan 处理。
func SumActiveChannelCostsByChannel(start, end int64) (map[int]int64, error) {
	rows := make([]struct {
		ChannelId int             `gorm:"column:channel_id"`
		Total     decimal.Decimal `gorm:"column:total"`
	}, 0)
	err := DB.Model(&ChannelCost{}).
		Select("channel_id, COALESCE(SUM(amount_cents), 0) AS total").
		Where("status = ? AND occurred_at >= ? AND occurred_at <= ?", ChannelCostStatusActive, start, end).
		Group("channel_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[int]int64, len(rows))
	for _, row := range rows {
		result[row.ChannelId] = row.Total.IntPart()
	}
	return result, nil
}

// FindDuplicateChannelCosts 检查同渠道、同金额、同一自然日的已有记录，用于防重复提示。
// 只提示不阻断：运营确实可能在同一天向同一上游付两笔相同金额，由操作者确认。
func FindDuplicateChannelCosts(channelId int, amountCents int64, dayStart, dayEnd int64) ([]int, error) {
	ids := make([]int, 0)
	err := DB.Model(&ChannelCost{}).
		Where("status = ? AND channel_id = ? AND amount_cents = ? AND occurred_at >= ? AND occurred_at < ?",
			ChannelCostStatusActive, channelId, amountCents, dayStart, dayEnd).
		Pluck("id", &ids).Error
	return ids, err
}

func fillChannelCostNames(items []ChannelCost) {
	seen := make(map[int]struct{})
	ids := make([]int, 0)
	for i := range items {
		id := items[i].ChannelId
		if id == 0 {
			items[i].ChannelName = "Unallocated"
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return
	}
	names := getChannelNames(ids)
	for i := range items {
		id := items[i].ChannelId
		if id == 0 {
			continue
		}
		if name, ok := names[id]; ok && name != "" {
			items[i].ChannelName = name
		} else {
			items[i].ChannelName = "channel-" + strconv.Itoa(id)
		}
	}
}
