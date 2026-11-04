package middleware

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// 规则五测试用到的两个小辅助函数。
//
// 单独放一个文件是为了让 video_route_failover_test.go 里只留「用例」本身，
// 读测试的人不用先翻辅助函数的实现。

// timeNowUnix 返回当前 Unix 秒。
//
// 抽出来是为了让「标记已过期 / 未过期」这类用例只依赖相对时间，
// 不依赖绝对时钟（跑得慢一点也不会让用例时好时坏）。
func timeNowUnix() int64 {
	return time.Now().Unix()
}

// assertModelRouted 断言改写后的请求体把模型换成了 want。
func assertModelRouted(t *testing.T, body string, want string) {
	t.Helper()
	var out map[string]interface{}
	if err := common.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal rewritten body: %v (%s)", err, body)
	}
	if out["model"] != want {
		t.Fatalf("model = %v, want %q (body=%s)", out["model"], want, body)
	}
}
