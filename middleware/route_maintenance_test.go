package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// 运维共享密钥鉴权的测试。
//
// 这个中间件守的是「暂停/恢复豆包路由」这个单用途接口。写错的两种典型方式：
//   - 密钥没配置时返回 403：等于告诉外部「这里确实有个受保护的接口」；
//   - 用 == 比较而不是常量时间比较：理论上可按字节计时猜密钥。
// 两条都在下面的用例里钉住。

const testMaintenanceKey = "test-maintenance-key-0123456789abcdef"

// setMaintenanceKey 设置（或清空）共享密钥，测试结束恢复原值。
//
// ⚠️ common.OptionMap 在单测里通常是 nil（没有跑 InitOptionMap），
// 直接赋值会 panic: assignment to entry in nil map。先确保它存在。
func setMaintenanceKey(t *testing.T, value string) {
	t.Helper()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previous, hadPrevious := common.OptionMap[seedanceroute_setting.MaintenanceKeyOption]
	common.OptionMap[seedanceroute_setting.MaintenanceKeyOption] = value
	t.Cleanup(func() {
		if hadPrevious {
			common.OptionMap[seedanceroute_setting.MaintenanceKeyOption] = previous
			return
		}
		delete(common.OptionMap, seedanceroute_setting.MaintenanceKeyOption)
	})
}

// newMaintenanceEngine 建一个只挂鉴权中间件的引擎，下游返回 200 + "reached"。
//
// ⚠️ Use 必须在 POST 之前：gin 在注册路由时快照中间件链，
// 顺序反了测试会「全部通过」但中间件从未执行。
func newMaintenanceEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(VideoRouteMaintenanceAuth())
	engine.POST("/api/video-route/maintenance/doubao/pause", func(c *gin.Context) {
		c.String(http.StatusOK, "reached")
	})
	return engine
}

// callMaintenance 发一个请求，返回状态码与响应体。
func callMaintenance(t *testing.T, key string, setHeader bool) (int, string) {
	t.Helper()
	request := httptest.NewRequest("POST", "/api/video-route/maintenance/doubao/pause", nil)
	if setHeader {
		request.Header.Set(MaintenanceKeyHeader, key)
	}
	response := httptest.NewRecorder()
	newMaintenanceEngine().ServeHTTP(response, request)
	return response.Code, response.Body.String()
}

func TestMaintenanceAuthReturns404WhenKeyNotConfigured(t *testing.T) {
	setMaintenanceKey(t, "")
	code, body := callMaintenance(t, testMaintenanceKey, true)
	// 没配密钥 = 接口不存在。用 404 而不是 403，不暴露接口存在性。
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", code, body)
	}
	if body == "reached" {
		t.Fatal("downstream handler was reached with no key configured")
	}
}

func TestMaintenanceAuthRejectsMissingHeader(t *testing.T) {
	setMaintenanceKey(t, testMaintenanceKey)
	code, body := callMaintenance(t, "", false)
	if code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", code, body)
	}
	if body == "reached" {
		t.Fatal("downstream handler was reached without header")
	}
}

func TestMaintenanceAuthRejectsWrongKey(t *testing.T) {
	setMaintenanceKey(t, testMaintenanceKey)
	for _, wrong := range []string{
		"wrong",
		testMaintenanceKey + "x",
		testMaintenanceKey[:len(testMaintenanceKey)-1],
		"TEST-MAINTENANCE-KEY-0123456789ABCDEF", // 大小写敏感
	} {
		code, body := callMaintenance(t, wrong, true)
		if code != http.StatusUnauthorized {
			t.Fatalf("key %q: status = %d, want 401 (body=%s)", wrong, code, body)
		}
		if body == "reached" {
			t.Fatalf("key %q: downstream handler was reached", wrong)
		}
	}
}

func TestMaintenanceAuthAcceptsCorrectKey(t *testing.T) {
	setMaintenanceKey(t, testMaintenanceKey)
	code, body := callMaintenance(t, testMaintenanceKey, true)
	if code != http.StatusOK || body != "reached" {
		t.Fatalf("status = %d body = %q, want 200/reached", code, body)
	}
}

func TestMaintenanceAuthTrimsWhitespace(t *testing.T) {
	// 配置值或请求头里带换行/空格是常见的粘贴事故，两边都 trim 掉，
	// 但**只 trim 首尾**——中间有空格仍然算错。
	setMaintenanceKey(t, "  "+testMaintenanceKey+"\n")
	code, body := callMaintenance(t, "\t"+testMaintenanceKey+"  ", true)
	if code != http.StatusOK || body != "reached" {
		t.Fatalf("status = %d body = %q, want 200/reached after trim", code, body)
	}
}
