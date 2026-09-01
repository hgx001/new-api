package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
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
