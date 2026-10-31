package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 回归守卫：模型名筛选必须能查到带首尾空格的输入。
//
// 线上事故（2026-11-01）：运营在「通用日志」的模型名输入框里粘贴了
// 「 seedance2.5-特惠」（带一个前导空格，界面上看不出来），页面显示
// 「未查询到日志」。同一时刻库里该模型有 4 条日志 —— 后端当时用
// `model_name = ?` 做等值匹配，空格参与比较，一条都命中不了。
//
// 这里锁两层行为：
//  1. 首尾空格必须被忽略（trim）
//  2. 只输一部分也要能搜到（包含匹配），否则「搜索」只是个等值过滤器
func TestGetUserLogs_模型名筛选忽略首尾空格且支持部分匹配(t *testing.T) {
	const userId = 9101
	fixtures := []Log{
		{UserId: userId, Username: "u9101", Type: LogTypeConsume, ModelName: "seedance2.5-特惠", Quota: 100},
		{UserId: userId, Username: "u9101", Type: LogTypeConsume, ModelName: "seedance-2-5", Quota: 200},
		{UserId: userId, Username: "u9101", Type: LogTypeConsume, ModelName: "wan3.0-video", Quota: 400},
	}
	for i := range fixtures {
		require.NoError(t, DB.Create(&fixtures[i]).Error)
	}

	cases := []struct {
		name      string
		modelName string
		want      int64
	}{
		{"原样全名", "seedance2.5-特惠", 1},
		{"前导空格（线上事故的输入）", " seedance2.5-特惠", 1},
		{"尾随空格", "seedance2.5-特惠 ", 1},
		{"前后都有空格", "  seedance2.5-特惠  ", 1},
		{"只有空格等于没筛", "   ", 3},
		{"部分名匹配同前缀的所有模型", "seedance", 2},
		{"部分名匹配单个模型", "wan3.0", 1},
		{"不存在的模型", "not-exist-model", 0},
		{"高级通配仍然可用", "%seedance%", 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, total, err := GetUserLogs(userId, LogTypeConsume, 0, 0, tc.modelName, "", 0, 20, "", "", "")
			require.NoError(t, err)
			assert.Equal(t, tc.want, total, "模型名 %q 的命中数不对", tc.modelName)
		})
	}
}

// 回归守卫：列表与统计必须用同一套模型名口径。
//
// 如果只改列表不改统计，会出现「列表能查到 3 条、统计显示 0 元」这种
// 更让人怀疑数据坏了的现象。两者都走 applyLogModelNameFilter，这里锁住。
func TestSumUsedQuota_模型名口径与列表一致(t *testing.T) {
	const userId = 9102
	// SumUsedQuota 不带用户维度（管理员口径，全站求和），同包其它用例也往
	// 同一个内存库写过日志 —— 所以这里用独有前缀，避免被别的用例污染。
	const exactName = "zzq-test-video-prime"
	const prefix = "zzq-test-video"
	rows := []Log{
		{UserId: userId, Username: "u9102", Type: LogTypeConsume, ModelName: exactName, Quota: 50},
		{UserId: userId, Username: "u9102", Type: LogTypeConsume, ModelName: prefix, Quota: 70},
	}
	for i := range rows {
		require.NoError(t, DB.Create(&rows[i]).Error)
	}

	// 带空格的精确名：统计也要命中，且只算这一个模型
	stat, err := SumUsedQuota(LogTypeConsume, 0, 0, " "+exactName+" ", "", "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, 50, stat.Quota)

	// 部分名：统计把同前缀的两个模型加在一起，与列表条数同口径
	stat, err = SumUsedQuota(LogTypeConsume, 0, 0, prefix, "", "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, 120, stat.Quota)

	_, total, err := GetUserLogs(userId, LogTypeConsume, 0, 0, prefix, "", 0, 20, "", "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
}

// 回归守卫：LIKE 元字符不能把筛选变成通配。
//
// 模型名里不会出现 % 和 _，但用户名/令牌名会（也支持这些列），
// 一旦不转义，输入 `_` 就会匹配任意单字符，筛选结果莫名其妙变多。
func TestApplyLogModelNameFilter_转义下划线(t *testing.T) {
	const userId = 9103
	rows := []Log{
		{UserId: userId, Username: "u9103", Type: LogTypeConsume, ModelName: "a_b-model", Quota: 1},
		{UserId: userId, Username: "u9103", Type: LogTypeConsume, ModelName: "axb-model", Quota: 2},
	}
	for i := range rows {
		require.NoError(t, DB.Create(&rows[i]).Error)
	}

	_, total, err := GetUserLogs(userId, LogTypeConsume, 0, 0, "a_b", "", 0, 20, "", "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 1, total, "下划线必须当字面量，不能匹配任意字符")
}
