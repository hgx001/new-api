package model

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ---------------------------------------------------------------------------
// 行锁：写法必须真的发出 FOR UPDATE
// ---------------------------------------------------------------------------

// TestClauseLockingEmitsForUpdateOnPostgres 证明 clause.Locking 会真的生成
// FOR UPDATE，而 gorm v1 时代的写法在 gorm v2 里被静默忽略（等于没锁）。
//
// 这条用例同时锁住两件事：
//  1. 正向：clause.Locking{Strength:"UPDATE"} → SQL 含 FOR UPDATE；
//  2. 反向对照：tx.Set("gorm:query_option","FOR UPDATE") → SQL 不含 FOR UPDATE
//     （这正是旧实现「看着像加了锁、其实没加」的根因）。
func TestClauseLockingEmitsForUpdateOnPostgres(t *testing.T) {
	db, err := gorm.Open(
		postgres.New(postgres.Config{
			DSN:                  "host=127.0.0.1 user=test dbname=test port=5432 sslmode=disable TimeZone=Asia/Shanghai",
			PreferSimpleProtocol: true,
		}),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true},
	)
	require.NoError(t, err)

	var rows []TopUp
	locked := db.Session(&gorm.Session{DryRun: true}).
		Model(&TopUp{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("trade_no = ?", "USR1ALIPAYLOCK").
		Find(&rows)
	require.NoError(t, locked.Error)
	require.Contains(t, locked.Statement.SQL.String(), "FOR UPDATE",
		"clause.Locking 必须真的生成 FOR UPDATE 行锁")

	var legacyRows []TopUp
	legacy := db.Session(&gorm.Session{DryRun: true}).
		Model(&TopUp{}).
		Set("gorm:"+"query_option", "FOR UPDATE").
		Where("trade_no = ?", "USR1ALIPAYLOCK").
		Find(&legacyRows)
	require.NoError(t, legacy.Error)
	require.NotContains(t, legacy.Statement.SQL.String(), "FOR UPDATE",
		"gorm v1 的写法在 gorm v2 会被静默忽略，不能再使用")
}

// TestModelPackageDoesNotUseIgnoredForUpdateOption 全仓扫描：model 包里不允许
// 再出现那个「看起来加了行锁、其实被静默忽略」的写法。
//
// 之所以用 AST 扫描而不是行为断言：SQLite 驱动不支持行锁，行为上观察不到差异；
// 而这里要防的正是「写法被淘汰」这种静默失效。用 AST 而不是字符串匹配，
// 是为了只抓真实的 Set(...) 调用、不误伤解释这段历史的注释。
func TestModelPackageDoesNotUseIgnoredForUpdateOption(t *testing.T) {
	const needle = "gorm:query_option"
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	var offenders []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(".", entry.Name())
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Set" || len(call.Args) == 0 {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			if value, unquoteErr := strconv.Unquote(literal.Value); unquoteErr == nil && value == needle {
				offenders = append(offenders, path)
			}
			return true
		})
	}
	require.Empty(t, offenders,
		"model 包里仍有使用失效行锁写法的文件（%v）：请改用 clause.Locking{Strength:\"UPDATE\"}", offenders)
}

// ---------------------------------------------------------------------------
// 管理员补单：幂等分支不得写出假「补单成功」日志
// ---------------------------------------------------------------------------

// TestManualCompleteTopUpDoesNotRecordFakeSuccessLog 锁定「只有真的加了额度才记日志」。
//
// 幂等分支（订单已是 success / 并发补单被抢）里 quotaToAdd 为 0、userId 为 0，
// 若照样写日志就会产生一条 user_id=0 的假「补单成功」记录，污染充值流水与对账。
func TestManualCompleteTopUpDoesNotRecordFakeSuccessLog(t *testing.T) {
	setupAlipayTopUpTestState(t)

	user := User{Id: 1, Username: "manual-complete-user", AffCode: "manual-complete-user", Password: "password", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)

	// 1) 正常补单：加额度 + 一条真实日志
	pending := TopUp{
		UserId:          user.Id,
		Amount:          10,
		Money:           73,
		TradeNo:         "USR1MANUALPENDING",
		PaymentMethod:   PaymentMethodAlipay,
		PaymentProvider: PaymentProviderAlipay,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(&pending).Error)
	require.NoError(t, ManualCompleteTopUp(pending.TradeNo, "127.0.0.1"))

	var gotUser User
	require.NoError(t, DB.First(&gotUser, user.Id).Error)
	assert.Equal(t, 5_000_000, gotUser.Quota)

	var successLogs int64
	require.NoError(t, DB.Model(&Log{}).
		Where("user_id = ? AND content LIKE ?", user.Id, "%管理员补单成功%").
		Count(&successLogs).Error)
	assert.Equal(t, int64(1), successLogs, "真实补单应记一条日志")

	// 2) 幂等重放：订单已是 success，不得再写任何「补单成功」日志
	require.NoError(t, ManualCompleteTopUp(pending.TradeNo, "127.0.0.1"))
	require.NoError(t, DB.First(&gotUser, user.Id).Error)
	assert.Equal(t, 5_000_000, gotUser.Quota, "幂等重放不得重复加额度")

	require.NoError(t, DB.Model(&Log{}).
		Where("content LIKE ?", "%管理员补单成功%").
		Count(&successLogs).Error)
	assert.Equal(t, int64(1), successLogs, "幂等重放不得产生第二条补单日志")

	// 3) 不得出现 user_id=0 的假日志
	var fakeLogs int64
	require.NoError(t, DB.Model(&Log{}).
		Where("user_id = 0 AND content LIKE ?", "%管理员补单成功%").
		Count(&fakeLogs).Error)
	assert.Equal(t, int64(0), fakeLogs, "不得产生 user_id=0 的假补单日志")
}
