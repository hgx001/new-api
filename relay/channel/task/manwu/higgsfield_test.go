package manwu

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHiggsfieldSpecRegistered(t *testing.T) {
	spec, ok := SpecFor(ModelHiggsfield25)
	if !ok {
		t.Fatalf("%s 未在 modelSpecs 注册", ModelHiggsfield25)
	}
	if spec.PlatformID != PlatformIDHiggsfield {
		t.Fatalf("platformId = %q, want %q", spec.PlatformID, PlatformIDHiggsfield)
	}
	if spec.OutputMode != OutputModeVideo {
		t.Fatalf("outputMode = %q, want video", spec.OutputMode)
	}
	if spec.UpstreamModel != "seedance-2.5" {
		t.Fatalf("upstream = %q, want seedance-2.5", spec.UpstreamModel)
	}
}

func TestResolveHiggsfieldDuration(t *testing.T) {
	cases := []struct {
		name string
		req  clientRequest
		want int
		ok   bool
	}{
		{"缺省 5 秒", clientRequest{}, 5, true},
		{"seconds=4 下界", clientRequest{Seconds: json.RawMessage(`4`)}, 4, true},
		{"duration=30 上界", clientRequest{Duration: json.RawMessage(`30`)}, 30, true},
		{"duration=16 逐秒", clientRequest{Duration: json.RawMessage(`"16"`)}, 16, true},
		{"3 秒越界", clientRequest{Duration: json.RawMessage(`3`)}, 0, false},
		{"31 秒越界", clientRequest{Duration: json.RawMessage(`31`)}, 0, false},
	}
	for _, c := range cases {
		got, err := resolveHiggsfieldDuration(c.req)
		if c.ok && err != nil {
			t.Fatalf("%s: 不应报错，got err=%v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Fatalf("%s: 应显式拒绝", c.name)
		}
		if c.ok && got != c.want {
			t.Fatalf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestHiggsfieldRejectsNonFixedResolution(t *testing.T) {
	// 分辨率固定 720p：显式传 1080p 必须 400，不能静默降成 720p 交付。
	if err := validateHiggsfieldResolution("1080p"); err == nil {
		t.Fatal("1080p 应被拒绝")
	}
	if err := validateHiggsfieldResolution(""); err != nil {
		t.Fatalf("空值应通过（缺省 720p）：%v", err)
	}
	if err := validateHiggsfieldResolution("720P"); err != nil {
		t.Fatalf("720P 应通过：%v", err)
	}
}

func TestHiggsfieldDescriptionMentionsFixedSpec(t *testing.T) {
	// 描述里的参数口径与产品说明一致（防止只改定价不改描述）
	sql, err := readFileForTest("../../../../scripts/prod-upsert-sd-seedance-25-20261125.sql")
	if err != nil {
		t.Skip("SQL 脚本不在当前工作目录")
	}
	for _, want := range []string{"sd-seedance-2.5", "720p", "0.034246575342", "4–30"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("上架脚本缺少 %q", want)
		}
	}
}
