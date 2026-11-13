package taskcommon

import (
	"context"
	"errors"
)

// ErrClientDisconnected 标记「下游客户端已经走了」。
//
// 为什么需要一个跨包的哨兵：适配器的 BuildRequestBody 只能返回 error，
// relay_task 拿到 error 无从判断「这是上游故障」还是「客户已经挂电话」。
// 前者该 500 + 重试 + 记渠道一次错，后者重试毫无意义（客户收不到结果），
// 还会把一次超时放大成两遍完整的素材下载、甚至把渠道打到 AutoBan。
//
// 适配器把自己的 ctx 取消包进这个哨兵即可；没包的 context.Canceled
// 也由 IsClientDisconnected 兜住，避免「哪个适配器写的」决定行为。
var ErrClientDisconnected = errors.New("client disconnected")

// IsClientDisconnected 判断错误链里是否含「客户端断开」。
//
// context.Canceled 一并算作断开：net/http 在连接关闭时取消 request ctx，
// 各适配器可能直接返回 ctx.Err() 或把它包进 io 错误。但
// context.DeadlineExceeded **不算** —— 那是我们等太久，仍要按超时处理，
// 不能被这条判定掩掉渠道故障。
func IsClientDisconnected(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrClientDisconnected) || errors.Is(err, context.Canceled)
}

// UserError 表示「请求本身不合法」：应返回 400，且**不归咎渠道**。
//
// 为什么需要它：适配器的 BuildRequestBody 只能返回 error，relay_task 会把任何
// error 包成 500 build_request_failed。而 500 会触发重试与 AutoBan——用户少传
// 一个 ratio 就把整条渠道打掉，是不可接受的副作用。
//
// dto.TaskError 不能直接当 error 用（它有一个名为 Error 的**字段**，值类型不
// 实现 error 接口），所以这里用一个真正实现 Error() string 的包装类型，让
// relay_task 能用 errors.As 识别出来并降级成 400 LocalError。
type UserError struct {
	Code    string
	Message string
	Err     error
}

func (e *UserError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "invalid request"
}

func (e *UserError) Unwrap() error { return e.Err }

// NewUserError 造一个「请求不合法」错误；code 为空时用 invalid_request。
func NewUserError(err error, code string) *UserError {
	if code == "" {
		code = "invalid_request"
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return &UserError{Code: code, Message: msg, Err: err}
}

var _ error = (*UserError)(nil)
