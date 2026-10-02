package taskcommon

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
