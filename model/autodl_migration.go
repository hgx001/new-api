// Copyright (C) 2023-2026 QuantumNous
//
// This file is part of New API.
//
// New API is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
)

func normalizeAutoDLChannelModels(channel *Channel) bool {
	if channel == nil {
		return false
	}
	channelType := channel.Type
	if channelType != constant.ChannelTypeAutoDL && channel.Id > 0 {
		var existing Channel
		if err := DB.Select("type").First(&existing, channel.Id).Error; err == nil {
			channelType = existing.Type
		}
	}
	if channelType != constant.ChannelTypeAutoDL {
		return false
	}

	models, modelsChanged := constant.MigrateAutoDLModelNames(channel.GetModels())
	channel.Models = strings.Join(models, ",")

	testModelChanged := false
	if channel.TestModel != nil {
		migrated, changed := constant.MigrateAutoDLModelNames([]string{*channel.TestModel})
		if changed && len(migrated) == 1 {
			channel.TestModel = &migrated[0]
			testModelChanged = true
		}
	}
	return modelsChanged || testModelChanged
}

func migrateAutoDLModelNames() error {
	var channels []Channel
	if err := DB.Where("type = ?", constant.ChannelTypeAutoDL).Find(&channels).Error; err != nil {
		return err
	}

	for i := range channels {
		channel := &channels[i]
		if !normalizeAutoDLChannelModels(channel) {
			continue
		}

		updates := map[string]any{"models": channel.Models}
		if channel.TestModel != nil {
			updates["test_model"] = channel.TestModel
		}
		err := DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&Channel{}).Where("id = ?", channel.Id).Updates(updates).Error; err != nil {
				return err
			}
			return channel.UpdateAbilities(tx)
		})
		if err != nil {
			return fmt.Errorf("migrate AutoDL channel %d model names: %w", channel.Id, err)
		}
	}
	return migrateAutoDLModelPriceOption()
}

func migrateAutoDLModelPriceOption() error {
	var option Option
	result := DB.Where("key = ?", "ModelPrice").First(&option)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil
	}
	if result.Error != nil {
		return result.Error
	}

	prices := make(map[string]float64)
	if err := common.UnmarshalJsonStr(option.Value, &prices); err != nil {
		return fmt.Errorf("decode ModelPrice option: %w", err)
	}

	changed := false
	for modelName, price := range prices {
		migrated, modelChanged := constant.MigrateAutoDLModelNames([]string{modelName})
		if !modelChanged || len(migrated) != 1 || migrated[0] == modelName {
			continue
		}
		if _, exists := prices[migrated[0]]; !exists {
			prices[migrated[0]] = price
		}
		delete(prices, modelName)
		changed = true
	}
	for modelName, defaultPrice := range ratio_setting.GetDefaultModelPriceMap() {
		if !strings.HasPrefix(modelName, "autodl:") {
			continue
		}
		if _, exists := prices[modelName]; exists {
			continue
		}
		prices[modelName] = defaultPrice
		changed = true
	}
	if !changed {
		return nil
	}

	value, err := common.Marshal(prices)
	if err != nil {
		return fmt.Errorf("encode ModelPrice option: %w", err)
	}
	return DB.Model(&Option{}).Where("key = ?", "ModelPrice").Update("value", string(value)).Error
}
