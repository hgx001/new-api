package model

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

// 云服务器租赁的订单、实例与操作记录。
//
// 资金口径（与 docs/agent-guides/payments.md 一致）：
//  1. 扣款与建单/建实例在**同一个事务**里，任何一步失败整体回滚；
//  2. 扣额度用条件更新（quota >= required），并发下只有一个请求能成功；
//  3. 释放退款按「实付金额 ÷ 已购时长」折算剩余价值，不按标价退，
//     否则折扣档（1 年 8.5 折）会在退订时被套利；
//  4. 所有金额以人民币分为单位算一次，再按汇率折成站内额度。

const (
	ServerInstanceStatusPending  = "pending"
	ServerInstanceStatusRunning  = "running"
	ServerInstanceStatusStopped  = "stopped"
	ServerInstanceStatusExpired  = "expired"
	ServerInstanceStatusReleased = "released"
)

const (
	ServerOrderTypePurchase = "purchase"
	ServerOrderTypeRenew    = "renew"
	ServerOrderTypeRefund   = "refund"
)

const (
	ServerProvisionSimulated = "simulated"
	ServerProvisionLive      = "live"
)

// 到期后保留多久再自动释放：这段时间内用户仍可续费救回，过期实例不可开机。
const ServerInstanceExpiryGraceSeconds int64 = 7 * 24 * 3600

// ServerOrderPaymentTimeout 是未支付订单的有效期（秒）。
//
// 超过这个时间还没收到网关回调，订单作废 —— 不然用户三个月后付款，
// 网关才慢悠悠回调，系统却按今天的目录价开一台机器。
const ServerOrderPaymentTimeout int64 = 2 * 3600

// roundMoneyCents 把金额收敛到「分」。float64 累加会漏出 ¥1.1199999999999999
// 这种尾巴，直接进账单会让用户看到诡异的小数。
func roundMoneyCents(v float64) float64 {
	return math.Round(v*100) / 100
}

var (
	ErrServerInstanceNotFound  = errors.New("实例不存在")
	ErrServerInstanceReleased  = errors.New("实例已释放")
	ErrServerInstanceExpired   = errors.New("实例已到期，请先续费")
	ErrServerInstanceNotActive = errors.New("实例当前状态不支持该操作")
	ErrServerQuotaInsufficient = errors.New("余额不足")
	ErrServerConcurrentUpdate  = errors.New("实例状态已变化，请刷新后重试")
	// ErrServerOrderNotFound 服务器订单不存在。
	ErrServerOrderNotFound = errors.New("订单不存在")
	// ErrServerOrderExpired 订单已超时作废。
	ErrServerOrderExpired = errors.New("订单已超时作废")
	// ErrServerPaymentProviderMismatch 支付网关与订单不匹配。
	ErrServerPaymentProviderMismatch = errors.New("支付网关与订单不匹配")
)

type ServerOrder struct {
	Id              int     `json:"id" gorm:"primaryKey"`
	UserId          int     `json:"user_id" gorm:"index:idx_server_order_user"`
	TradeNo         string  `json:"trade_no" gorm:"uniqueIndex;type:varchar(64)"`
	ClientRequestId *string `json:"-" gorm:"uniqueIndex;type:varchar(64)"`
	Type            string  `json:"type" gorm:"type:varchar(16);index"`
	InstanceId      int     `json:"instance_id" gorm:"index"`
	InstanceName    string  `json:"instance_name" gorm:"type:varchar(64)"`
	Plan            string  `json:"plan" gorm:"type:varchar(32)"`
	SpecId          string  `json:"spec_id" gorm:"type:varchar(64)"`
	Region          string  `json:"region" gorm:"type:varchar(32)"`
	Mbps            int     `json:"mbps"`
	DurationId      string  `json:"duration_id" gorm:"type:varchar(16)"`
	Billing         string  `json:"billing" gorm:"type:varchar(16)"`
	Amount          float64 `json:"amount"`      // 人民币；退款为负数
	QuotaDelta      int     `json:"quota_delta"` // 站内额度；退款为负数
	PaymentMethod   string  `json:"payment_method" gorm:"type:varchar(32)"`
	// PaymentProvider 区分「站内余额」与「第三方网关」。
	// 为空或 balance 表示站内余额扣款（同步完成）；epay 表示走易支付，
	// 订单先落库为 pending，等回调验签通过才置成功并开通实例。
	PaymentProvider string `json:"payment_provider" gorm:"type:varchar(32)"`
	Status          string `json:"status" gorm:"type:varchar(16);index"`
	Description     string `json:"description" gorm:"type:varchar(255)"`
	CreateTime      int64  `json:"create_time" gorm:"index"`
	CompleteTime    int64  `json:"complete_time"`
	// Config 与 Spec 是下单时的配置/规格快照。
	//
	// 第三方支付是**异步**的：下单那一刻实例还不存在，等回调到达才创建。
	// 所以快照必须存在订单上，否则回调时已经不知道该开什么机器了。
	Config string `json:"-" gorm:"type:text"`
	Spec   string `json:"-" gorm:"type:text"`
	// UnitPrice 是下单时的单价；回调开通实例时直接用它，避免目录改价后
	// 「用户付的价」与「算出来的价」对不上。
	UnitPrice     float64 `json:"unit_price"`
	PeriodSeconds int64   `json:"period_seconds"`
	// TradeNoExpires 是未支付订单的作废时间；超时后回调不再开通实例。
	TradeNoExpires int64 `json:"-" gorm:"column:trade_no_expires"`
}

type ServerInstance struct {
	Id               int     `json:"id" gorm:"primaryKey"`
	UserId           int     `json:"user_id" gorm:"index:idx_server_instance_user"`
	Name             string  `json:"name" gorm:"uniqueIndex;type:varchar(64)"`
	OrderId          int     `json:"order_id" gorm:"index"`
	Config           string  `json:"-" gorm:"type:text"` // setting.ServerConfig JSON
	Spec             string  `json:"-" gorm:"type:text"` // setting.ServerSpec 快照
	Region           string  `json:"region" gorm:"type:varchar(32)"`
	Plan             string  `json:"plan" gorm:"type:varchar(32)"`
	SpecId           string  `json:"spec_id" gorm:"type:varchar(64)"`
	Mbps             int     `json:"mbps"`
	DurationId       string  `json:"duration_id" gorm:"type:varchar(16)"`
	Billing          string  `json:"billing" gorm:"type:varchar(16)"`
	UnitPrice        float64 `json:"unit_price"` // 下单时的单价（¥/月 或 ¥/小时）
	PaidAmount       float64 `json:"paid_amount"`
	RefundedAmount   float64 `json:"refunded_amount"`
	PurchasedSeconds int64   `json:"purchased_seconds"`
	IP               string  `json:"ip" gorm:"type:varchar(64)"`
	Username         string  `json:"username" gorm:"type:varchar(32)"`
	// Password 存的是 AES-GCM 密文：控制台要回显，但不能明文落库。
	Password      string `json:"-" gorm:"type:text"`
	Status        string `json:"status" gorm:"type:varchar(16);index"`
	ProvisionMode string `json:"provision_mode" gorm:"type:varchar(16)"`
	ExpireAt      int64  `json:"expire_at" gorm:"index"`
	CreatedAt     int64  `json:"created_at" gorm:"index"`
	UpdatedAt     int64  `json:"updated_at"`
	ReleasedAt    int64  `json:"released_at"`
}

type ServerInstanceEvent struct {
	Id         int    `json:"id" gorm:"primaryKey"`
	InstanceId int    `json:"instance_id" gorm:"index"`
	UserId     int    `json:"user_id" gorm:"index"`
	Action     string `json:"action" gorm:"type:varchar(32)"`
	Detail     string `json:"detail" gorm:"type:varchar(255)"`
	CreatedAt  int64  `json:"created_at" gorm:"index"`
}

// ServerInstanceView 是下发给控制台的实例视图（含解密后的密码与规格快照）。
type ServerInstanceView struct {
	Id               int                  `json:"id"`
	Name             string               `json:"name"`
	Status           string               `json:"status"`
	Ip               string               `json:"ip"`
	Username         string               `json:"username"`
	Password         string               `json:"password"`
	PasswordReadable bool                 `json:"password_readable"`
	Config           setting.ServerConfig `json:"config"`
	Spec             setting.ServerSpec   `json:"spec"`
	Region           string               `json:"region"`
	Plan             string               `json:"plan"`
	Mbps             int                  `json:"mbps"`
	Billing          string               `json:"billing"`
	UnitPrice        float64              `json:"unit_price"`
	PaidAmount       float64              `json:"paid_amount"`
	RefundedAmount   float64              `json:"refunded_amount"`
	ProvisionMode    string               `json:"provision_mode"`
	ExpireAt         int64                `json:"expire_at"`
	CreatedAt        int64                `json:"created_at"`
	ReleasedAt       int64                `json:"released_at"`
	RemainingSeconds int64                `json:"remaining_seconds"`
	OrderId          int                  `json:"order_id"`
}

func (instance *ServerInstance) ToView(now int64) ServerInstanceView {
	var config setting.ServerConfig
	if instance.Config != "" {
		_ = common.UnmarshalJsonStr(instance.Config, &config)
	}
	var spec setting.ServerSpec
	if instance.Spec != "" {
		_ = common.UnmarshalJsonStr(instance.Spec, &spec)
	}
	password, err := common.DecryptSecret(instance.Password)
	view := ServerInstanceView{
		Id:               instance.Id,
		Name:             instance.Name,
		Status:           instance.Status,
		Ip:               instance.IP,
		Username:         instance.Username,
		Password:         password,
		PasswordReadable: err == nil && password != "",
		Config:           config,
		Spec:             spec,
		Region:           instance.Region,
		Plan:             instance.Plan,
		Mbps:             instance.Mbps,
		Billing:          instance.Billing,
		UnitPrice:        roundMoneyCents(instance.UnitPrice),
		PaidAmount:       roundMoneyCents(instance.PaidAmount),
		RefundedAmount:   roundMoneyCents(instance.RefundedAmount),
		ProvisionMode:    instance.ProvisionMode,
		ExpireAt:         instance.ExpireAt,
		CreatedAt:        instance.CreatedAt,
		ReleasedAt:       instance.ReleasedAt,
		RemainingSeconds: instance.ExpireAt - now,
		OrderId:          instance.OrderId,
	}
	if view.RemainingSeconds < 0 {
		view.RemainingSeconds = 0
	}
	return view
}

func generateServerTradeNo(orderType string, userId int) string {
	prefix := "SRV"
	if orderType == ServerOrderTypeRenew {
		prefix = "SRNR"
	}
	if orderType == ServerOrderTypeRefund {
		prefix = "SRRF"
	}
	return fmt.Sprintf("%s%d%s%d", prefix, userId, strings.ToUpper(common.GetRandomString(10)), time.Now().UnixNano()%100000)
}

func randomHex(length int) string {
	buf := make([]byte, (length+1)/2)
	if _, err := rand.Read(buf); err != nil {
		return common.GetRandomString(length)
	}
	value := hex.EncodeToString(buf)
	if len(value) > length {
		value = value[:length]
	}
	return value
}

// generateServerPassword 生成 20 位强密码：四类字符各至少一个。
func generateServerPassword() string {
	const upper = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	const lower = "abcdefghijkmnpqrstuvwxyz"
	const digits = "23456789"
	const symbols = "!@#$%^&*-_=+"
	all := upper + lower + digits + symbols

	pick := func(chars string) byte {
		buf := make([]byte, 1)
		if _, err := rand.Read(buf); err != nil {
			return chars[time.Now().UnixNano()%int64(len(chars))]
		}
		return chars[int(buf[0])%len(chars)]
	}

	chars := []byte{pick(upper), pick(lower), pick(digits), pick(symbols)}
	for len(chars) < 20 {
		chars = append(chars, pick(all))
	}
	for i := len(chars) - 1; i > 0; i-- {
		buf := make([]byte, 1)
		if _, err := rand.Read(buf); err != nil {
			break
		}
		j := int(buf[0]) % (i + 1)
		chars[i], chars[j] = chars[j], chars[i]
	}
	return string(chars)
}

// simulatedServerIP 用 RFC 5737 的文档保留段 198.51.100.0/24。
//
// 模拟开台没有真实机器，绝不能发一个看起来能连的公网 IP —— 那可能是别人的主机。
// 198.51.100.0/24 是专门留给文档/示例的地址，永远不会路由到真实设备。
func simulatedServerIP() string {
	buf := make([]byte, 1)
	if _, err := rand.Read(buf); err != nil {
		return "198.51.100.10"
	}
	return fmt.Sprintf("198.51.100.%d", int(buf[0])%254+1)
}

func generateServerInstanceName(plan string) string {
	return fmt.Sprintf("%s-%s-%s", plan, time.Now().Format("0102"), randomHex(4))
}

func serverInstanceNameExists(tx *gorm.DB, name string) bool {
	var count int64
	if err := tx.Model(&ServerInstance{}).Where("name = ?", name).Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

// PurchaseServerInstance 用余额购买一台服务器。
//
// 幂等：clientRequestId 非空时，同一请求号只会成交一次（前端每次打开收银台
// 生成一个，双击/重试不会买出两台机器）。返回的 order 可能是历史订单，
// 此时 instance 也一并返回，调用方无需区分。
func PurchaseServerInstance(
	userId int,
	config setting.ServerConfig,
	quote *setting.ServerQuote,
	paymentMethod string,
	clientRequestId string,
) (*ServerOrder, *ServerInstance, error) {
	return purchaseServerInstanceWithProvider(
		userId, config, quote, paymentMethod, PaymentProviderBalance, clientRequestId,
	)
}

// PurchaseServerInstanceWithProvider 按指定支付网关下单。
//
// provider 为 PaymentProviderBalance 时同步扣额度并开通实例；
// 为 PaymentProviderAlipay 时只落 pending 订单，等支付宝回调后开通
// （见 CompleteServerOrderPaid）。
func PurchaseServerInstanceWithProvider(
	userId int,
	config setting.ServerConfig,
	quote *setting.ServerQuote,
	paymentMethod string,
	provider string,
	clientRequestId string,
) (*ServerOrder, *ServerInstance, error) {
	return purchaseServerInstanceWithProvider(
		userId, config, quote, paymentMethod, provider, clientRequestId,
	)
}

// purchaseServerInstanceWithProvider 是购买流程的共同实现。
//
// provider 决定扣款方式：
//   - PaymentProviderBalance：同步扣站内额度，订单直接成功、实例立即创建；
//   - PaymentProviderAlipay：同步阶段**只落一张 pending 订单**，不扣额度也不建实例；
//     真正的开通动作发生在支付宝回调里（见 CompleteServerOrderPaid）。
func purchaseServerInstanceWithProvider(
	userId int,
	config setting.ServerConfig,
	quote *setting.ServerQuote,
	paymentMethod string,
	provider string,
	clientRequestId string,
) (*ServerOrder, *ServerInstance, error) {
	if userId <= 0 || quote == nil {
		return nil, nil, errors.New("参数错误")
	}
	requiredQuota := setting.ServerQuotaForAmount(quote.Total)
	if requiredQuota <= 0 {
		return nil, nil, errors.New("订单金额不合法")
	}
	now := common.GetTimestamp()

	var order *ServerOrder
	var instance *ServerInstance
	var cachedQuota int

	err := DB.Transaction(func(tx *gorm.DB) error {
		if clientRequestId != "" {
			var existing ServerOrder
			err := tx.Where("client_request_id = ? AND user_id = ?", clientRequestId, userId).First(&existing).Error
			if err == nil {
				order = &existing
				// 幂等重放：把已开通的实例也带回来，调用方无需区分「新建」与「重放」。
				var existingInstance ServerInstance
				if err := tx.Where("order_id = ?", existing.Id).First(&existingInstance).Error; err == nil {
					instance = &existingInstance
				}
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		configJson, err := common.Marshal(config)
		if err != nil {
			return err
		}
		plan, ok := setting.GetServerPlanById(config.Plan)
		if !ok {
			return fmt.Errorf("未知机型: %s", config.Plan)
		}
		spec, ok := setting.GetServerSpec(plan, config.SpecId)
		if !ok {
			return fmt.Errorf("未知规格: %s", config.SpecId)
		}
		specJson, err := common.Marshal(spec)
		if err != nil {
			return err
		}

		// 第三方支付：只落一张 pending 订单，不扣额度也不建实例。
		// 开通动作要等网关回调（见 CompleteServerOrderPaid）。
		if provider != PaymentProviderBalance {
			order = &ServerOrder{
				UserId:          userId,
				TradeNo:         generateServerTradeNo(ServerOrderTypePurchase, userId),
				Type:            ServerOrderTypePurchase,
				Plan:            config.Plan,
				SpecId:          config.SpecId,
				Region:          config.Region,
				Mbps:            config.Mbps,
				DurationId:      config.DurationId,
				Billing:         quote.Billing,
				Amount:          quote.Total,
				QuotaDelta:      0,
				PaymentMethod:   paymentMethod,
				PaymentProvider: provider,
				Status:          common.TopUpStatusPending,
				Description:     fmt.Sprintf("购买云服务器 %s / %s", config.Plan, config.SpecId),
				Config:          string(configJson),
				Spec:            string(specJson),
				UnitPrice:       quote.UnitPrice,
				PeriodSeconds:   quote.PeriodSeconds,
				CreateTime:      now,
				TradeNoExpires:  now + ServerOrderPaymentTimeout,
			}
			if clientRequestId != "" {
				id := clientRequestId
				order.ClientRequestId = &id
			}
			return tx.Create(order).Error
		}

		res := tx.Model(&User{}).
			Where("id = ? AND quota >= ?", userId, requiredQuota).
			Update("quota", gorm.Expr("quota - ?", requiredQuota))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			var user User
			if err := tx.Select("id", "quota").Where("id = ?", userId).First(&user).Error; err != nil {
				return err
			}
			return ErrServerQuotaInsufficient
		}
		var user User
		if err := tx.Select("quota").Where("id = ?", userId).First(&user).Error; err != nil {
			return err
		}
		cachedQuota = user.Quota

		order = &ServerOrder{
			UserId:          userId,
			TradeNo:         generateServerTradeNo(ServerOrderTypePurchase, userId),
			Type:            ServerOrderTypePurchase,
			Plan:            config.Plan,
			SpecId:          config.SpecId,
			Region:          config.Region,
			Mbps:            config.Mbps,
			DurationId:      config.DurationId,
			Billing:         quote.Billing,
			Amount:          quote.Total,
			QuotaDelta:      -requiredQuota,
			PaymentMethod:   paymentMethod,
			PaymentProvider: provider,
			Status:          common.TopUpStatusSuccess,
			Description:     fmt.Sprintf("购买云服务器 %s / %s", config.Plan, config.SpecId),
			Config:          string(configJson),
			Spec:            string(specJson),
			UnitPrice:       quote.UnitPrice,
			PeriodSeconds:   quote.PeriodSeconds,
			CreateTime:      now,
			CompleteTime:    now,
		}
		if clientRequestId != "" {
			id := clientRequestId
			order.ClientRequestId = &id
		}
		if err := tx.Create(order).Error; err != nil {
			return err
		}

		name := generateServerInstanceName(config.Plan)
		for attempts := 0; attempts < 5 && serverInstanceNameExists(tx, name); attempts++ {
			name = generateServerInstanceName(config.Plan)
		}
		encryptedPassword, err := common.EncryptSecret(generateServerPassword())
		if err != nil {
			return err
		}
		instance = &ServerInstance{
			UserId:           userId,
			Name:             name,
			OrderId:          order.Id,
			Config:           string(configJson),
			Spec:             string(specJson),
			Region:           config.Region,
			Plan:             config.Plan,
			SpecId:           spec.Id,
			Mbps:             config.Mbps,
			DurationId:       config.DurationId,
			Billing:          quote.Billing,
			UnitPrice:        quote.UnitPrice,
			PaidAmount:       quote.Total,
			PurchasedSeconds: quote.PeriodSeconds,
			IP:               simulatedServerIP(),
			Username:         "ubuntu",
			Password:         encryptedPassword,
			Status:           ServerInstanceStatusRunning,
			ProvisionMode:    ServerProvisionSimulated,
			ExpireAt:         now + quote.PeriodSeconds,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := tx.Create(instance).Error; err != nil {
			return err
		}
		order.InstanceId = instance.Id
		order.InstanceName = instance.Name
		if err := tx.Model(order).Updates(map[string]interface{}{
			"instance_id":   instance.Id,
			"instance_name": instance.Name,
		}).Error; err != nil {
			return err
		}

		return tx.Create(&ServerInstanceEvent{
			InstanceId: instance.Id,
			UserId:     userId,
			Action:     "create",
			// Detail 只放**语言中性**的事实（实例名、订单号、金额），
			// 具体描述由前端按 action 翻译 —— 否则英文界面会蹦出中文。
			Detail:    fmt.Sprintf("%s · %s", instance.Name, order.TradeNo),
			CreatedAt: now,
		}).Error
	})
	if err != nil {
		return nil, nil, err
	}

	// 第三方支付的订单此刻还没有实例：额度没扣、机器没开，
	// 等网关回调成功后再开通（见 CompleteServerOrderPaid）。
	if provider != PaymentProviderBalance {
		return order, nil, nil
	}

	if cachedQuota > 0 {
		if err := updateUserQuotaCache(userId, cachedQuota); err != nil {
			common.SysLog("failed to refresh user quota cache after server purchase: " + err.Error())
		}
	}
	RecordLog(userId, LogTypeTopup, fmt.Sprintf(
		"购买云服务器 %s，订单 %s，金额 ¥%.2f，扣除额度 %d",
		instance.Name, order.TradeNo, order.Amount, -order.QuotaDelta,
	))
	return order, instance, nil
}

// CompleteServerOrderPaid 处理第三方支付的到账回调：把 pending 订单置成功并开通实例。
//
// 这是**整个支付链路最关键的一步**，必须满足：
//  1. 幂等：网关会重放通知（合法签名、可重放报文），同一笔订单只能开通一次；
//  2. 金额校验：回调上报的金额必须与订单金额一致（都按分比对）——
//     否则「用 0.01 元买 199 元的机器」也会发货；
//  3. 状态流转用条件更新（CAS）+ 行锁，全局只有一次转换权；
//  4. 开通实例与订单置成功在**同一个事务**里，不会出现「订单成功但没机器」
//     或「机器开了但订单还是 pending」；
//  5. 过期订单不再开通 —— 用户付了钱但超时太久，宁可人工补单也不能按今天的
//     目录价开一台（目录价可能已经变了）。
//
// completed=false 表示这笔订单此前已被处理过（幂等重放）。
func CompleteServerOrderPaid(
	tradeNo string,
	provider string,
	paidAmount float64,
	actualPaymentMethod string,
) (completed bool, instance *ServerInstance, err error) {
	if tradeNo == "" {
		return false, nil, errors.New("未提供订单号")
	}
	now := common.GetTimestamp()

	var order ServerOrder
	var createdInstance *ServerInstance

	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrServerOrderNotFound
			}
			return err
		}
		if order.PaymentProvider != provider {
			return ErrServerPaymentProviderMismatch
		}
		if order.Type != ServerOrderTypePurchase {
			return errors.New("订单类型不支持该操作")
		}
		if order.Status == common.TopUpStatusSuccess {
			// 幂等：已开通过，把实例带回去即可。
			var existing ServerInstance
			if err := tx.Where("order_id = ?", order.Id).First(&existing).Error; err == nil {
				createdInstance = &existing
			}
			return nil
		}
		if order.Status != common.TopUpStatusPending {
			return errors.New("订单状态错误")
		}
		// 金额必须一致（都按分比）。
		expected := roundMoneyCents(order.Amount)
		actual := roundMoneyCents(paidAmount)
		if expected != actual {
			return fmt.Errorf("订单金额不一致：期望 ¥%.2f，实际 ¥%.2f", expected, actual)
		}
		if order.TradeNoExpires > 0 && now > order.TradeNoExpires {
			return ErrServerOrderExpired
		}

		// CAS：只有把 pending 改成 success 的那一个请求能拿到转换权。
		res := tx.Model(&ServerOrder{}).
			Where("id = ? AND status = ?", order.Id, common.TopUpStatusPending).
			Updates(map[string]interface{}{
				"status":        common.TopUpStatusSuccess,
				"complete_time": now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			// 并发：另一路已开通，幂等返回
			var existing ServerInstance
			if err := tx.Where("order_id = ?", order.Id).First(&existing).Error; err == nil {
				createdInstance = &existing
			}
			return nil
		}
		// 拿到转换权的就是本请求：它负责开通实例。
		completed = true

		if actualPaymentMethod != "" && actualPaymentMethod != order.PaymentMethod {
			// 用户下单选支付宝、实际用微信付：记下实际渠道，对账用。
			if err := tx.Model(&ServerOrder{}).Where("id = ?", order.Id).
				Update("payment_method", actualPaymentMethod).Error; err != nil {
				return err
			}
			order.PaymentMethod = actualPaymentMethod
		}

		// 按订单里的配置快照开通实例。
		//
		// 用订单快照而不是重新查目录：用户付款时的价格与配置就是承诺，
		// 中间目录改价不能影响已成交的订单。
		var config setting.ServerConfig
		if order.Config != "" {
			if err := common.UnmarshalJsonStr(order.Config, &config); err != nil {
				return err
			}
		}
		spec := setting.ServerSpec{Id: order.SpecId}
		if order.Spec != "" {
			_ = common.UnmarshalJsonStr(order.Spec, &spec)
		}

		name := generateServerInstanceName(order.Plan)
		for attempts := 0; attempts < 5 && serverInstanceNameExists(tx, name); attempts++ {
			name = generateServerInstanceName(order.Plan)
		}
		encryptedPassword, err := common.EncryptSecret(generateServerPassword())
		if err != nil {
			return err
		}
		created := &ServerInstance{
			UserId:           order.UserId,
			Name:             name,
			OrderId:          order.Id,
			Config:           order.Config,
			Spec:             order.Spec,
			Region:           order.Region,
			Plan:             order.Plan,
			SpecId:           order.SpecId,
			Mbps:             order.Mbps,
			DurationId:       order.DurationId,
			Billing:          order.Billing,
			UnitPrice:        order.UnitPrice,
			PaidAmount:       order.Amount,
			PurchasedSeconds: order.PeriodSeconds,
			IP:               simulatedServerIP(),
			Username:         "ubuntu",
			Password:         encryptedPassword,
			Status:           ServerInstanceStatusRunning,
			ProvisionMode:    ServerProvisionSimulated,
			ExpireAt:         now + order.PeriodSeconds,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := tx.Create(created).Error; err != nil {
			return err
		}
		createdInstance = created

		if err := tx.Model(&ServerOrder{}).Where("id = ?", order.Id).
			Updates(map[string]interface{}{
				"instance_id":   created.Id,
				"instance_name": created.Name,
			}).Error; err != nil {
			return err
		}

		return tx.Create(&ServerInstanceEvent{
			InstanceId: created.Id,
			UserId:     order.UserId,
			Action:     "create",
			Detail:     fmt.Sprintf("%s · %s", created.Name, order.TradeNo),
			CreatedAt:  now,
		}).Error
	})
	if err != nil {
		return false, nil, err
	}
	if completed && createdInstance != nil {
		RecordLog(order.UserId, LogTypeTopup, fmt.Sprintf(
			"在线支付购买云服务器 %s，订单 %s，金额 ¥%.2f",
			createdInstance.Name, order.TradeNo, order.Amount,
		))
	}
	return completed, createdInstance, nil
}

// GetServerOrderByTradeNo 按订单号查订单。
func GetServerOrderByTradeNo(tradeNo string) *ServerOrder {
	var order ServerOrder
	if err := DB.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return nil
	}
	return &order
}

// GetServerOrderById 按主键查订单（限用户）。
func GetServerOrderById(userId, orderId int) *ServerOrder {
	var order ServerOrder
	if err := DB.Where("id = ? AND user_id = ?", orderId, userId).First(&order).Error; err != nil {
		return nil
	}
	return &order
}

// ExpireServerOrders 把超时未支付的订单置为 failed。
//
// 网关回调若在作废之后才到达，会被 CompleteServerOrderPaid 拒绝（状态非 pending），
// 用户需要走人工补单 —— 这是刻意的：宁可人工介入，也不按过期目录价发货。
func ExpireServerOrders(now int64) (int64, error) {
	res := DB.Model(&ServerOrder{}).
		Where("status = ? AND type = ?", common.TopUpStatusPending, ServerOrderTypePurchase).
		Where("trade_no_expires > 0 AND trade_no_expires <= ?", now).
		Update("status", common.TopUpStatusFailed)
	return res.RowsAffected, res.Error
}

// 续费订单的 SpecId 取实例快照里的规格 id（实例表有独立列，便于查账）。

// RenewServerInstance 续费一台实例：从「当前到期时间」与「现在」的较晚者起算。
//
// 已到期但还在宽限期内的实例仍可续费救回 —— 这是云厂商的通行做法，
// 到期立刻销毁会让用户来不及补钱。
func RenewServerInstance(
	userId int,
	instanceId int,
	quote *setting.ServerQuote,
	paymentMethod string,
	clientRequestId string,
) (*ServerOrder, *ServerInstance, error) {
	if userId <= 0 || instanceId <= 0 || quote == nil {
		return nil, nil, errors.New("参数错误")
	}
	requiredQuota := setting.ServerQuotaForAmount(quote.Total)
	if requiredQuota <= 0 {
		return nil, nil, errors.New("订单金额不合法")
	}
	now := common.GetTimestamp()

	var order *ServerOrder
	var instance *ServerInstance
	var cachedQuota int

	err := DB.Transaction(func(tx *gorm.DB) error {
		if clientRequestId != "" {
			var existing ServerOrder
			err := tx.Where("client_request_id = ? AND user_id = ?", clientRequestId, userId).First(&existing).Error
			if err == nil {
				var current ServerInstance
				if err := tx.Where("id = ?", existing.InstanceId).First(&current).Error; err != nil {
					return err
				}
				order, instance = &existing, &current
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		var current ServerInstance
		if err := tx.Where("id = ? AND user_id = ?", instanceId, userId).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrServerInstanceNotFound
			}
			return err
		}
		if current.Status == ServerInstanceStatusReleased {
			return ErrServerInstanceReleased
		}
		base := current.ExpireAt
		if base < now {
			base = now
		}
		newExpireAt := base + quote.PeriodSeconds

		res := tx.Model(&ServerInstance{}).
			Where("id = ? AND expire_at = ?", current.Id, current.ExpireAt).
			Updates(map[string]interface{}{
				"expire_at":         newExpireAt,
				"status":            ServerInstanceStatusRunning,
				"paid_amount":       gorm.Expr("paid_amount + ?", quote.Total),
				"purchased_seconds": gorm.Expr("purchased_seconds + ?", quote.PeriodSeconds),
				"updated_at":        now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrServerConcurrentUpdate
		}

		res = tx.Model(&User{}).
			Where("id = ? AND quota >= ?", userId, requiredQuota).
			Update("quota", gorm.Expr("quota - ?", requiredQuota))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrServerQuotaInsufficient
		}
		var user User
		if err := tx.Select("quota").Where("id = ?", userId).First(&user).Error; err != nil {
			return err
		}
		cachedQuota = user.Quota

		order = &ServerOrder{
			UserId:        userId,
			TradeNo:       generateServerTradeNo(ServerOrderTypeRenew, userId),
			Type:          ServerOrderTypeRenew,
			InstanceId:    current.Id,
			InstanceName:  current.Name,
			Plan:          current.Plan,
			SpecId:        current.SpecId,
			Region:        current.Region,
			Mbps:          current.Mbps,
			DurationId:    quote.DurationId,
			Billing:       quote.Billing,
			Amount:        quote.Total,
			QuotaDelta:    -requiredQuota,
			PaymentMethod: paymentMethod,
			Status:        common.TopUpStatusSuccess,
			Description:   fmt.Sprintf("续费 %s（%s）", current.Name, quote.DurationId),
			CreateTime:    now,
			CompleteTime:  now,
		}
		if clientRequestId != "" {
			id := clientRequestId
			order.ClientRequestId = &id
		}
		if err := tx.Create(order).Error; err != nil {
			return err
		}

		if err := tx.Create(&ServerInstanceEvent{
			InstanceId: current.Id,
			UserId:     userId,
			Action:     "renew",
			Detail:     fmt.Sprintf("%s · ¥%.2f", quote.DurationId, quote.Total),
			CreatedAt:  now,
		}).Error; err != nil {
			return err
		}

		instance = &current
		instance.ExpireAt = newExpireAt
		instance.Status = ServerInstanceStatusRunning
		// 金额按分收敛：float64 累加会漏出 ¥1.1199999999999999 这种尾巴
		instance.PaidAmount = roundMoneyCents(instance.PaidAmount + quote.Total)
		instance.PurchasedSeconds += quote.PeriodSeconds
		instance.UpdatedAt = now
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	if cachedQuota > 0 {
		if err := updateUserQuotaCache(userId, cachedQuota); err != nil {
			common.SysLog("failed to refresh user quota cache after server renewal: " + err.Error())
		}
	}
	RecordLog(userId, LogTypeTopup, fmt.Sprintf(
		"续费云服务器 %s，订单 %s，金额 ¥%.2f", instance.Name, order.TradeNo, order.Amount,
	))
	return order, instance, nil
}

// serverRefundQuota 计算退订应退额度。
//
// 按「实付 ÷ 已购时长」得到每秒实际成本，再乘剩余秒数 —— 用实付而不是标价，
// 否则 1 年 8.5 折买的机器用一天就退，按标价退会多退钱。
// 上限是「实付 - 已退」，保证任何情况下不会倒贴。
func serverRefundQuota(instance *ServerInstance, now int64) (float64, int) {
	remaining := instance.ExpireAt - now
	if remaining <= 0 || instance.PurchasedSeconds <= 0 || instance.PaidAmount <= 0 {
		return 0, 0
	}
	if remaining > instance.PurchasedSeconds {
		remaining = instance.PurchasedSeconds
	}
	// 先把历史金额归一到分再参与计算：库里可能存着 1.1199999999999999 这种
	// 早期累加留下的尾巴，直接参与除法会让退款多出或少掉一厘。
	paid := roundMoneyCents(instance.PaidAmount)
	refunded := roundMoneyCents(instance.RefundedAmount)
	perSecond := paid / float64(instance.PurchasedSeconds)
	refundAmount := perSecond * float64(remaining)
	maxRefund := roundMoneyCents(paid - refunded)
	if refundAmount > maxRefund {
		refundAmount = maxRefund
	}
	refundAmount = roundMoneyCents(refundAmount)
	if refundAmount <= 0 {
		return 0, 0
	}
	return refundAmount, setting.ServerQuotaForAmount(refundAmount)
}

// ReleaseServerInstance 释放实例，并按剩余时长退回余额。
//
// 释放是**不可逆**的：实例进入 released 终态，控制台不再展示为可用资源，
// 但订单与操作记录保留，用户事后仍能查到「买过、用过、退了多少」。
func ReleaseServerInstance(userId, instanceId int) (*ServerOrder, *ServerInstance, error) {
	if userId <= 0 || instanceId <= 0 {
		return nil, nil, errors.New("参数错误")
	}
	now := common.GetTimestamp()

	var instance *ServerInstance
	var refundOrder *ServerOrder
	var cachedQuota int

	err := DB.Transaction(func(tx *gorm.DB) error {
		var current ServerInstance
		if err := tx.Where("id = ? AND user_id = ?", instanceId, userId).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrServerInstanceNotFound
			}
			return err
		}
		if current.Status == ServerInstanceStatusReleased {
			return ErrServerInstanceReleased
		}

		refundAmount, refundQuota := serverRefundQuota(&current, now)
		res := tx.Model(&ServerInstance{}).
			Where("id = ? AND status = ?", current.Id, current.Status).
			Updates(map[string]interface{}{
				"status":          ServerInstanceStatusReleased,
				"released_at":     now,
				"updated_at":      now,
				"refunded_amount": gorm.Expr("refunded_amount + ?", refundAmount),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrServerConcurrentUpdate
		}

		if refundQuota > 0 {
			if err := tx.Model(&User{}).Where("id = ?", userId).
				Update("quota", gorm.Expr("quota + ?", refundQuota)).Error; err != nil {
				return err
			}
			var user User
			if err := tx.Select("quota").Where("id = ?", userId).First(&user).Error; err != nil {
				return err
			}
			cachedQuota = user.Quota
			refundOrder = &ServerOrder{
				UserId:        userId,
				TradeNo:       generateServerTradeNo(ServerOrderTypeRefund, userId),
				Type:          ServerOrderTypeRefund,
				InstanceId:    current.Id,
				InstanceName:  current.Name,
				Plan:          current.Plan,
				Region:        current.Region,
				Mbps:          current.Mbps,
				DurationId:    current.DurationId,
				Billing:       current.Billing,
				Amount:        -refundAmount,
				QuotaDelta:    refundQuota,
				PaymentMethod: PaymentMethodBalance,
				Status:        common.TopUpStatusSuccess,
				Description:   fmt.Sprintf("释放 %s 退回剩余价值", current.Name),
				CreateTime:    now,
				CompleteTime:  now,
			}
			if err := tx.Create(refundOrder).Error; err != nil {
				return err
			}
		}

		if err := tx.Create(&ServerInstanceEvent{
			InstanceId: current.Id,
			UserId:     userId,
			Action:     "release",
			Detail:     fmt.Sprintf("¥%.2f", refundAmount),
			CreatedAt:  now,
		}).Error; err != nil {
			return err
		}

		instance = &current
		instance.Status = ServerInstanceStatusReleased
		instance.ReleasedAt = now
		instance.RefundedAmount = roundMoneyCents(
			instance.RefundedAmount + refundAmount,
		)
		instance.UpdatedAt = now
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	if cachedQuota > 0 {
		if err := updateUserQuotaCache(userId, cachedQuota); err != nil {
			common.SysLog("failed to refresh user quota cache after server release: " + err.Error())
		}
	}
	RecordLog(userId, LogTypeTopup, fmt.Sprintf(
		"释放云服务器 %s，退款 ¥%.2f", instance.Name, instance.RefundedAmount,
	))
	return refundOrder, instance, nil
}

// SetServerInstancePower 开机 / 关机。
//
// 停机不释放实例，也不延长租期：到期时间不变，用户随时可以再开机。
func SetServerInstancePower(userId, instanceId int, start bool) (*ServerInstance, error) {
	now := common.GetTimestamp()
	var instance ServerInstance
	if err := DB.Where("id = ? AND user_id = ?", instanceId, userId).First(&instance).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrServerInstanceNotFound
		}
		return nil, err
	}
	if instance.Status == ServerInstanceStatusReleased {
		return nil, ErrServerInstanceReleased
	}
	if instance.ExpireAt <= now || instance.Status == ServerInstanceStatusExpired {
		return nil, ErrServerInstanceExpired
	}

	target := ServerInstanceStatusStopped
	action := "stop"
	if start {
		target = ServerInstanceStatusRunning
		action = "start"
	}
	if instance.Status == target {
		return &instance, nil
	}
	res := DB.Model(&ServerInstance{}).
		Where("id = ? AND status = ?", instance.Id, instance.Status).
		Updates(map[string]interface{}{"status": target, "updated_at": now})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected != 1 {
		return nil, ErrServerConcurrentUpdate
	}
	instance.Status = target
	instance.UpdatedAt = now
	_ = DB.Create(&ServerInstanceEvent{
		InstanceId: instance.Id,
		UserId:     userId,
		Action:     action,
		Detail:     "",
		CreatedAt:  now,
	}).Error
	return &instance, nil
}

// RebootServerInstance 重启：仅运行中的实例可重启，状态不变但记录一次操作。
func RebootServerInstance(userId, instanceId int) (*ServerInstance, error) {
	now := common.GetTimestamp()
	var instance ServerInstance
	if err := DB.Where("id = ? AND user_id = ?", instanceId, userId).First(&instance).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrServerInstanceNotFound
		}
		return nil, err
	}
	if instance.Status == ServerInstanceStatusReleased {
		return nil, ErrServerInstanceReleased
	}
	if instance.ExpireAt <= now || instance.Status == ServerInstanceStatusExpired {
		return nil, ErrServerInstanceExpired
	}
	if instance.Status != ServerInstanceStatusRunning {
		return nil, ErrServerInstanceNotActive
	}
	if err := DB.Model(&ServerInstance{}).Where("id = ?", instance.Id).
		Update("updated_at", now).Error; err != nil {
		return nil, err
	}
	instance.UpdatedAt = now
	_ = DB.Create(&ServerInstanceEvent{
		InstanceId: instance.Id,
		UserId:     userId,
		Action:     "reboot",
		Detail:     "",
		CreatedAt:  now,
	}).Error
	return &instance, nil
}

// ResetServerInstancePassword 重置登录密码，返回新密码（仅本次响应中明文出现）。
func ResetServerInstancePassword(userId, instanceId int, newPassword string) (string, error) {
	if len(newPassword) < 8 {
		return "", errors.New("密码至少 8 位")
	}
	now := common.GetTimestamp()
	var instance ServerInstance
	if err := DB.Where("id = ? AND user_id = ?", instanceId, userId).First(&instance).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrServerInstanceNotFound
		}
		return "", err
	}
	if instance.Status == ServerInstanceStatusReleased {
		return "", ErrServerInstanceReleased
	}
	encrypted, err := common.EncryptSecret(newPassword)
	if err != nil {
		return "", err
	}
	if err := DB.Model(&ServerInstance{}).Where("id = ?", instance.Id).
		Updates(map[string]interface{}{"password": encrypted, "updated_at": now}).Error; err != nil {
		return "", err
	}
	_ = DB.Create(&ServerInstanceEvent{
		InstanceId: instance.Id,
		UserId:     userId,
		Action:     "reset_password",
		Detail:     "",
		CreatedAt:  now,
	}).Error
	return newPassword, nil
}

func GetServerInstance(userId, instanceId int) (*ServerInstance, error) {
	var instance ServerInstance
	if err := DB.Where("id = ? AND user_id = ?", instanceId, userId).First(&instance).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrServerInstanceNotFound
		}
		return nil, err
	}
	return &instance, nil
}

func ListServerInstances(userId int, includeReleased bool) ([]*ServerInstance, error) {
	var instances []*ServerInstance
	query := DB.Where("user_id = ?", userId)
	if !includeReleased {
		query = query.Where("status <> ?", ServerInstanceStatusReleased)
	}
	err := query.Order("created_at desc, id desc").Limit(200).Find(&instances).Error
	return instances, err
}

func ListServerOrders(userId int, limit int) ([]*ServerOrder, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var orders []*ServerOrder
	err := DB.Where("user_id = ?", userId).Order("create_time desc, id desc").Limit(limit).Find(&orders).Error
	return orders, err
}

func ListServerInstanceEvents(userId, instanceId int, limit int) ([]*ServerInstanceEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var events []*ServerInstanceEvent
	err := DB.Where("user_id = ? AND instance_id = ?", userId, instanceId).
		Order("created_at desc, id desc").Limit(limit).Find(&events).Error
	return events, err
}

// ExpireServerInstances 把到期实例标记为 expired，并把超过宽限期的实例自动释放。
//
// 由后台任务每分钟调用一次，同时列表接口也会先调一次 —— 否则用户点开控制台
// 到定时任务下一次触发之间，会看到一台「已过期但仍显示运行中」的机器。
func ExpireServerInstances(now int64) (int64, int64, error) {
	expired := DB.Model(&ServerInstance{}).
		Where("status IN ?", []string{ServerInstanceStatusRunning, ServerInstanceStatusStopped}).
		Where("expire_at <= ?", now).
		Update("status", ServerInstanceStatusExpired)
	if expired.Error != nil {
		return 0, 0, expired.Error
	}

	released := DB.Model(&ServerInstance{}).
		Where("status = ?", ServerInstanceStatusExpired).
		Where("expire_at <= ?", now-ServerInstanceExpiryGraceSeconds).
		Updates(map[string]interface{}{
			"status":      ServerInstanceStatusReleased,
			"released_at": now,
			"updated_at":  now,
		})
	if released.Error != nil {
		return expired.RowsAffected, 0, released.Error
	}
	return expired.RowsAffected, released.RowsAffected, nil
}

// StartServerExpiryWorker 启动后台到期扫描。
func StartServerExpiryWorker() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if _, _, err := ExpireServerInstances(common.GetTimestamp()); err != nil {
				common.SysError("failed to expire server instances: " + err.Error())
			}
		}
	}()
}
