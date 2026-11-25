package constant

import "testing"

func TestIsHiggsfieldFamily(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"sd-seedance-2.5", true},
		{"  sd-seedance-2.5  ", true},
		// 近名商品必须互不命中（不同上游/不同价）
		{"sd-seedance-2.0", false},
		{"sd-seedance-2.0fast", false},
		{"cp-seedance-2.5", false},
		{"el-seedance-2.5", false},
		{"seedance-2-5", false},
		{"seedance-2-5-flash", false},
		{"seedance-2.0", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsHiggsfieldFamily(c.model); got != c.want {
			t.Fatalf("IsHiggsfieldFamily(%q) = %v, want %v", c.model, got, c.want)
		}
	}
}

func TestIsHiggsfieldPerSecond(t *testing.T) {
	if !IsHiggsfieldPerSecond("sd-seedance-2.5") {
		t.Fatal("sd-seedance-2.5 必须按秒计费")
	}
	if IsHiggsfieldPerSecond("sd-seedance-2.0") {
		t.Fatal("sd-seedance-2.0 属于 TikTok 族，不应命中 Higgsfield 族")
	}
}
