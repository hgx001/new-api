package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupVendorDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Model{}, &Vendor{}))

	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })
}

// SyncModelVendors 只补「从没归类过」的行，已归类的一律不动。
func TestSyncModelVendors_只补未归类的(t *testing.T) {
	setupVendorDB(t)

	require.NoError(t, DB.Create(&Model{ModelName: "wan3.0-video-官网", Status: 1}).Error)
	require.NoError(t, DB.Create(&Model{ModelName: "seedance-2-5", Status: 1}).Error)
	// 手工指定过的供应商不能被自动规则覆盖
	require.NoError(t, DB.Create(&Model{ModelName: "seedance-2.0", Status: 1, VendorID: 999}).Error)
	require.NoError(t, DB.Create(&Model{ModelName: "totally-unknown", Status: 1}).Error)

	updated, err := SyncModelVendors()
	require.NoError(t, err)
	assert.Equal(t, 2, updated, "只补 wan3 与未知的 seedance；手工指定的不动，未知的不猜")

	var wan Model
	require.NoError(t, DB.Where("model_name = ?", "wan3.0-video-官网").First(&wan).Error)
	assert.NotZero(t, wan.VendorID)

	var vendor Vendor
	require.NoError(t, DB.First(&vendor, wan.VendorID).Error)
	assert.Equal(t, "阿里巴巴", vendor.Name)
	assert.Equal(t, "Qwen.Color", vendor.Icon)

	var manual Model
	require.NoError(t, DB.Where("model_name = ?", "seedance-2.0").First(&manual).Error)
	assert.Equal(t, 999, manual.VendorID, "手工指定的供应商必须原样保留")

	var unknown Model
	require.NoError(t, DB.Where("model_name = ?", "totally-unknown").First(&unknown).Error)
	assert.Zero(t, unknown.VendorID, "没有规则命中的模型保持未归类")

	// 幂等：再跑一次没有可补的了
	updated, err = SyncModelVendors()
	require.NoError(t, err)
	assert.Equal(t, 0, updated)
}

// ReclassifyModelVendors 会纠正已经归错类的模型。
func TestReclassifyModelVendors_纠正误分类(t *testing.T) {
	setupVendorDB(t)

	byteDance := Vendor{Name: "字节跳动", Status: 1, Icon: "Doubao.Color"}
	require.NoError(t, byteDance.Insert())
	require.NoError(t, DB.Create(&Model{ModelName: "Nano Banana Pro", Status: 1, VendorID: byteDance.Id}).Error)

	updated, err := ReclassifyModelVendors()
	require.NoError(t, err)
	assert.Equal(t, 1, updated)

	var m Model
	require.NoError(t, DB.Where("model_name = ?", "Nano Banana Pro").First(&m).Error)
	var v Vendor
	require.NoError(t, DB.First(&v, m.VendorID).Error)
	assert.Equal(t, "Google", v.Name, "Nano Banana Pro 是 Google 的模型，不能挂在字节跳动下")
}
