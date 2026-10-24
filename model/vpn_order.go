package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// VpnOrder VPN 年费套餐订单。
// 密钥字段 NodeKey 服务端保存，任何接口都不会在订单未支付时下发。
type VpnOrder struct {
	Id              int     `json:"id"`
	UserId          int     `json:"user_id" gorm:"index"`
	TradeNo         string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	Plan            string  `json:"plan" gorm:"type:varchar(50)"`
	Money           float64 `json:"money"`
	PaymentMethod   string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider string  `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	Status          string  `json:"status"`
	CreateTime      int64   `json:"create_time"`
	CompleteTime    int64   `json:"complete_time"`
	NodeKey         string  `json:"-" gorm:"type:text"`
}

func (o *VpnOrder) Insert() error {
	return DB.Create(o).Error
}

func (o *VpnOrder) Update() error {
	return DB.Save(o).Error
}

func GetVpnOrderByTradeNo(tradeNo string) *VpnOrder {
	var o VpnOrder
	err := DB.Where("trade_no = ?", tradeNo).First(&o).Error
	if err != nil {
		return nil
	}
	return &o
}

// GetUserVpnOrders 返回用户全部订单（按创建时间倒序）。
func GetUserVpnOrders(userId int) ([]*VpnOrder, error) {
	var orders []*VpnOrder
	err := DB.Where("user_id = ?", userId).Order("create_time desc").Limit(50).Find(&orders).Error
	return orders, err
}

// GetLatestPaidVpnOrder 返回用户最近一笔已支付订单。
func GetLatestPaidVpnOrder(userId int) (*VpnOrder, error) {
	var o VpnOrder
	err := DB.Where("user_id = ? AND status = ?", userId, common.TopUpStatusSuccess).
		Order("complete_time desc").First(&o).Error
	if err != nil {
		return nil, errors.New("no paid vpn order")
	}
	return &o, nil
}

// HasPaidVpnOrder 判断用户是否已有生效的年费订单。
func HasPaidVpnOrder(userId int) (bool, error) {
	var count int64
	err := DB.Model(&VpnOrder{}).Where("user_id = ? AND status = ?", userId, common.TopUpStatusSuccess).Count(&count).Error
	return count > 0, err
}

// CompleteVpnOrderPaid 把一笔 VPN 订单从 pending 原子地置为 success。
//
// 并发安全：支付网关的异步通知是**可重放的合法签名**（签名伪造不了，但报文能重放），
// 用户点「返回」与异步通知也可能同时到达。因此「pending → success」必须全局只转换一次：
//  1. 用 gorm v2 的 clause.Locking 真正加行锁（gorm v1 时代的 gorm:query_option 在 v2
//     已被移除，写上去会被静默忽略，等于没锁）；
//  2. 状态流转用条件更新（CAS）并以 RowsAffected 判定，拿到转换权的那一个才算成功；
//  3. 进程内再由 controller 侧 LockOrder 兜一层（与充值链路保持一致）。
//
// 金额校验放在事务内做：回调上报的金额必须与订单金额一致（都按「舍入到分」比），
// 否则「用户用 0.01 元买了 199 元的套餐」也会被置成功。
//
// completed=false 表示该订单此前已被其它并发/重放回调处理（幂等，不重复置成功）。
// extraUpdates 用于回写网关回传的**实际**支付方式等附加字段。
func CompleteVpnOrderPaid(
	tradeNo string,
	provider string,
	expectedAmount decimal.Decimal,
	completeTime int64,
	extraUpdates map[string]interface{},
) (completed bool, err error) {
	if tradeNo == "" {
		return false, errors.New("未提供 VPN 订单号")
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		refCol := "`trade_no`"
		if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
			refCol = `"trade_no"`
		}
		var o VpnOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(refCol+" = ?", tradeNo).First(&o).Error; err != nil {
			return errors.New("VPN 订单不存在")
		}
		if o.PaymentProvider != provider {
			return ErrPaymentMethodMismatch
		}
		if o.Status == common.TopUpStatusSuccess {
			// 幂等：已经置成功，直接返回（不重复写 complete_time）
			return nil
		}
		if o.Status != common.TopUpStatusPending {
			return errors.New("VPN 订单状态错误")
		}
		expected := decimal.NewFromFloat(o.Money).Round(2)
		if !expected.Equal(expectedAmount.Round(2)) {
			return fmt.Errorf("VPN 订单金额不一致：expected=%s actual=%s", expected.StringFixed(2), expectedAmount.StringFixed(2))
		}
		updates := map[string]interface{}{
			"status":        common.TopUpStatusSuccess,
			"complete_time": completeTime,
		}
		for k, v := range extraUpdates {
			updates[k] = v
		}
		// 条件更新（CAS）：只有把 pending 改成 success 的那一个请求才能拿到转换权
		res := tx.Model(&VpnOrder{}).
			Where("trade_no = ? AND status = ?", tradeNo, common.TopUpStatusPending).
			Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		completed = res.RowsAffected == 1
		return nil
	})
	return completed, err
}
