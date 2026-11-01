package taskcommon

import (
	"strings"
	"testing"
)

// RedactMaterialURL 必须把 base64 data URL 压成摘要，不能把图片字节流
// 原样写进错误信息与日志 —— 那等于在网关日志里落了一份用户的图片原文件。
func TestRedactMaterialURLRedactsDataURL(t *testing.T) {
	payload := strings.Repeat("A", 5000)
	got := RedactMaterialURL("data:image/png;base64," + payload)

	if strings.Contains(got, payload) {
		t.Fatalf("data URL payload leaked into redacted output: %q", got)
	}
	if strings.Contains(got, "AAAA") {
		t.Fatalf("data URL payload leaked into redacted output: %q", got)
	}
	if !strings.Contains(got, "data:image/png") {
		t.Fatalf("redacted output should keep the MIME type, got %q", got)
	}
	if !strings.Contains(got, "base64") {
		t.Fatalf("redacted output should keep the encoding hint, got %q", got)
	}
	if !strings.Contains(got, "bytes") {
		t.Fatalf("redacted output should report the payload size, got %q", got)
	}
}

// 普通 URL 去掉 query string（签名参数不该进日志），再截断。
func TestRedactMaterialURLStripsQueryString(t *testing.T) {
	got := RedactMaterialURL("https://img.example.com/a.png?token=secret&sign=abc")

	if strings.Contains(got, "secret") || strings.Contains(got, "sign=") {
		t.Fatalf("query string leaked into redacted output: %q", got)
	}
	if !strings.Contains(got, "img.example.com/a.png") {
		t.Fatalf("redacted output should keep the path, got %q", got)
	}
}

// 超长路径也要截断，避免错误信息本身变成攻击面。
func TestRedactMaterialURLTruncatesLongPath(t *testing.T) {
	long := "https://img.example.com/" + strings.Repeat("x", 500)
	got := RedactMaterialURL(long)

	if len(got) > 200 {
		t.Fatalf("redacted output should be truncated, got %d chars", len(got))
	}
}

// 畸形 data URL（没有逗号）不能 panic，也不能吐出一大段原文。
func TestRedactMaterialURLHandlesMalformedDataURL(t *testing.T) {
	got := RedactMaterialURL("data:image/png;base64")

	if strings.Contains(got, "base64") && len(got) > 40 {
		t.Fatalf("malformed data URL should not be echoed, got %q", got)
	}
}

// 非 URL 的相对路径也要被截断。
func TestRedactMaterialURLTruncatesRelativePath(t *testing.T) {
	got := RedactMaterialURL(strings.Repeat("a", 500))

	if len(got) > 200 {
		t.Fatalf("redacted output should be truncated, got %d chars", len(got))
	}
}
