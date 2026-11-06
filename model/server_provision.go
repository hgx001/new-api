/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

package model

// 人工开台：把「已付款」变成「机器可用」的那一步交给管理员。
//
// 背景：机器是人工准备的（选机房、装系统、配网络），不可能付款瞬间就绪。
// 所以支付回调只建订单与实例骨架，凭据与 IP 由管理员在后台填写，
// 点「分配」后才下发到用户控制台。这样既不会让用户看到一台空壳机器，
// 也把「谁在什么时候交付了这台机器」留进了审计链路。

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

// ServerAssignInput 是管理员在「服务器分配」里填写的交付信息。
//
// 密码与私钥至少填一个：有人只给密钥、有人只用密码，一刀切会逼着管理员
// 去别处造一个假密码。两个都填则都下发，用户自己选一种登录。
type ServerAssignInput struct {
	IP         string `json:"ip"`
	SshPort    int    `json:"ssh_port"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	PrivateKey string `json:"private_key"`
	Note       string `json:"note"`
}

func (input ServerAssignInput) normalized() ServerAssignInput {
	out := input
	out.IP = strings.TrimSpace(out.IP)
	out.Username = strings.TrimSpace(out.Username)
	out.Note = strings.TrimSpace(out.Note)
	out.PrivateKey = strings.TrimSpace(out.PrivateKey)
	if out.SshPort == 0 {
		out.SshPort = 22
	}
	return out
}

func (input ServerAssignInput) validate() error {
	if net.ParseIP(input.IP) == nil {
		return fmt.Errorf("%w：IP 地址格式不正确", ErrServerAssignInvalid)
	}
	if input.SshPort < 1 || input.SshPort > 65535 {
		return fmt.Errorf("%w：SSH 端口需在 1-65535 之间", ErrServerAssignInvalid)
	}
	if input.Username == "" || len(input.Username) > 32 {
		return fmt.Errorf("%w：登录用户名不能为空且不超过 32 字符", ErrServerAssignInvalid)
	}
	if input.Password == "" && input.PrivateKey == "" {
		return fmt.Errorf("%w：密码与 SSH 私钥至少填写一项", ErrServerAssignInvalid)
	}
	if len(input.Password) > 256 {
		return fmt.Errorf("%w：密码过长", ErrServerAssignInvalid)
	}
	if len(input.PrivateKey) > 8000 {
		return fmt.Errorf("%w：私钥过长", ErrServerAssignInvalid)
	}
	if len([]rune(input.Note)) > 200 {
		return fmt.Errorf("%w：备注不超过 200 字", ErrServerAssignInvalid)
	}
	return nil
}

// AssignServerOrder 把一台待分配的机器交付给用户。
//
// 分配是「订单已付款」之后唯一的开通动作：
//   - 实例状态从 provisioning 变为 running；
//   - **到期时间从此刻起算**（now + 已购时长），排队等待的时间不计费；
//   - 凭据加密落库，明文只在这一次请求里出现过。
//
// 用 CAS（provision_status = awaiting）抢占：两个管理员同时点分配，
// 只有一个能成功，另一个拿 ErrServerOrderNotAwaiting，不会覆盖已写的凭据。
func AssignServerOrder(tradeNo string, adminId int, input ServerAssignInput) (*ServerOrder, *ServerInstance, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" || adminId <= 0 {
		return nil, nil, ErrServerAssignInvalid
	}
	input = input.normalized()
	if err := input.validate(); err != nil {
		return nil, nil, err
	}
	now := common.GetTimestamp()

	encryptedPassword := ""
	if input.Password != "" {
		value, err := common.EncryptSecret(input.Password)
		if err != nil {
			return nil, nil, err
		}
		encryptedPassword = value
	}
	encryptedKey := ""
	if input.PrivateKey != "" {
		value, err := common.EncryptSecret(input.PrivateKey)
		if err != nil {
			return nil, nil, err
		}
		encryptedKey = value
	}

	var order ServerOrder
	var instance *ServerInstance
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrServerOrderNotFound
			}
			return err
		}
		if order.Type != ServerOrderTypePurchase {
			return ErrServerOrderNotAwaiting
		}
		if order.Status != common.TopUpStatusSuccess {
			return ErrServerOrderNotAwaiting
		}
		if order.ProvisionStatus != ServerOrderProvisionAwaiting {
			return ErrServerOrderNotAwaiting
		}

		var current ServerInstance
		if err := tx.Where("order_id = ?", order.Id).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrServerInstanceNotFound
			}
			return err
		}
		if current.Status != ServerInstanceStatusProvisioning {
			// 已经被别的入口开过了，别覆盖已交付的凭据。
			return ErrServerOrderNotAwaiting
		}

		res := tx.Model(&ServerInstance{}).
			Where("id = ? AND status = ?", current.Id, ServerInstanceStatusProvisioning).
			Updates(map[string]interface{}{
				"ip":             input.IP,
				"ssh_port":       input.SshPort,
				"username":       input.Username,
				"password":       encryptedPassword,
				"private_key":    encryptedKey,
				"admin_note":     input.Note,
				"status":         ServerInstanceStatusRunning,
				"provision_mode": ServerProvisionLive,
				"expire_at":      now + current.PurchasedSeconds,
				"assigned_by":    adminId,
				"assigned_at":    now,
				"updated_at":     now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrServerConcurrentUpdate
		}

		res = tx.Model(&ServerOrder{}).
			Where("id = ? AND provision_status = ?", order.Id, ServerOrderProvisionAwaiting).
			Updates(map[string]interface{}{
				"provision_status": ServerOrderProvisionAssigned,
				"provisioned_at":   now,
				"provisioned_by":   adminId,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrServerOrderNotAwaiting
		}

		current.IP = input.IP
		current.SshPort = input.SshPort
		current.Username = input.Username
		current.Password = encryptedPassword
		current.PrivateKey = encryptedKey
		current.AdminNote = input.Note
		current.Status = ServerInstanceStatusRunning
		current.ProvisionMode = ServerProvisionLive
		current.ExpireAt = now + current.PurchasedSeconds
		current.AssignedBy = adminId
		current.AssignedAt = now
		current.UpdatedAt = now
		instance = &current

		order.ProvisionStatus = ServerOrderProvisionAssigned
		order.ProvisionedAt = now
		order.ProvisionedBy = adminId

		return tx.Create(&ServerInstanceEvent{
			InstanceId: current.Id,
			UserId:     current.UserId,
			Action:     "assign",
			Detail:     fmt.Sprintf("%s · %s", current.Name, input.IP),
			CreatedAt:  now,
		}).Error
	})
	if err != nil {
		return nil, nil, err
	}

	RecordLog(order.UserId, LogTypeTopup, fmt.Sprintf(
		"管理员分配云服务器 %s，IP %s，订单 %s", instance.Name, input.IP, order.TradeNo,
	))
	return &order, instance, nil
}

// ServerProvisionOrderView 是管理端「服务器分配」列表的一行。
//
// 特意把用户信息与规格快照一起带出去：管理员填凭据时要能看见
// 「这是谁的机器、买的是什么配置、付了多少钱」，否则得来回跳页面核对。
type ServerProvisionOrderView struct {
	TradeNo         string               `json:"trade_no"`
	UserId          int                  `json:"user_id"`
	Username        string               `json:"username"`
	Email           string               `json:"email"`
	Plan            string               `json:"plan"`
	SpecId          string               `json:"spec_id"`
	Region          string               `json:"region"`
	Mbps            int                  `json:"mbps"`
	DurationId      string               `json:"duration_id"`
	Billing         string               `json:"billing"`
	Amount          float64              `json:"amount"`
	Status          string               `json:"status"`
	ProvisionStatus string               `json:"provision_status"`
	PaidAt          int64                `json:"paid_at"`
	ProvisionedAt   int64                `json:"provisioned_at"`
	WaitedSeconds   int64                `json:"waited_seconds"`
	InstanceId      int                  `json:"instance_id"`
	InstanceName    string               `json:"instance_name"`
	Config          setting.ServerConfig `json:"config"`
	Spec            setting.ServerSpec   `json:"spec"`
}

// ListServerProvisionOrders 列出购买订单的交付进度。
//
// status 传 awaiting 只看待办，传 assigned 只看已交付，空则两者都返回。
// 待办按支付时间升序 —— 先付的先办，不能让早付款的人排在后面。
func ListServerProvisionOrders(status string, limit int) ([]*ServerProvisionOrderView, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := DB.Model(&ServerOrder{}).
		Where("type = ?", ServerOrderTypePurchase).
		Where("status = ?", common.TopUpStatusSuccess)
	switch status {
	case ServerOrderProvisionAwaiting:
		query = query.Where("provision_status = ?", ServerOrderProvisionAwaiting).
			Order("complete_time asc")
	case ServerOrderProvisionAssigned:
		query = query.Where("provision_status = ?", ServerOrderProvisionAssigned).
			Order("provisioned_at desc")
	default:
		query = query.Where("provision_status IN ?", []string{
			ServerOrderProvisionAwaiting, ServerOrderProvisionAssigned,
		}).Order("provisioned_at desc, complete_time asc")
	}

	var orders []*ServerOrder
	if err := query.Limit(limit).Find(&orders).Error; err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return []*ServerProvisionOrderView{}, nil
	}

	userIds := make([]int, 0, len(orders))
	for _, order := range orders {
		userIds = append(userIds, order.UserId)
	}
	var users []User
	if err := DB.Select("id, username, email").Where("id IN ?", userIds).Find(&users).Error; err != nil {
		return nil, err
	}
	userMap := make(map[int]User, len(users))
	for _, user := range users {
		userMap[user.Id] = user
	}

	now := common.GetTimestamp()
	views := make([]*ServerProvisionOrderView, 0, len(orders))
	for _, order := range orders {
		var config setting.ServerConfig
		if order.Config != "" {
			_ = common.UnmarshalJsonStr(order.Config, &config)
		}
		var spec setting.ServerSpec
		if order.Spec != "" {
			_ = common.UnmarshalJsonStr(order.Spec, &spec)
		}
		waited := int64(0)
		if order.ProvisionStatus == ServerOrderProvisionAwaiting {
			waited = now - order.CompleteTime
			if waited < 0 {
				waited = 0
			}
		}
		user := userMap[order.UserId]
		views = append(views, &ServerProvisionOrderView{
			TradeNo:         order.TradeNo,
			UserId:          order.UserId,
			Username:        user.Username,
			Email:           user.Email,
			Plan:            order.Plan,
			SpecId:          order.SpecId,
			Region:          order.Region,
			Mbps:            order.Mbps,
			DurationId:      order.DurationId,
			Billing:         order.Billing,
			Amount:          roundMoneyCents(order.Amount),
			Status:          order.Status,
			ProvisionStatus: order.ProvisionStatus,
			PaidAt:          order.CompleteTime,
			ProvisionedAt:   order.ProvisionedAt,
			WaitedSeconds:   waited,
			InstanceId:      order.InstanceId,
			InstanceName:    order.InstanceName,
			Config:          config,
			Spec:            spec,
		})
	}
	return views, nil
}

// ServerProvisionSummary 是管理端角标用的计数：待办数量 + 最久等待秒数。
//
// oldest 让前端能把「已经等了 40 分钟」直接标红，不必先拉一次列表
// 才知道积压有多严重。
func ServerProvisionSummary() (awaiting int64, oldestWaitSeconds int64, err error) {
	var row struct {
		Total  int64
		Oldest int64
	}
	err = DB.Model(&ServerOrder{}).
		Select("COUNT(*) AS total, COALESCE(MIN(complete_time), 0) AS oldest").
		Where("type = ?", ServerOrderTypePurchase).
		Where("status = ?", common.TopUpStatusSuccess).
		Where("provision_status = ?", ServerOrderProvisionAwaiting).
		Scan(&row).Error
	if err != nil {
		return 0, 0, err
	}
	awaiting = row.Total
	if row.Oldest > 0 {
		oldestWaitSeconds = common.GetTimestamp() - row.Oldest
		if oldestWaitSeconds < 0 {
			oldestWaitSeconds = 0
		}
	}
	return awaiting, oldestWaitSeconds, nil
}
