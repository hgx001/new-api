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
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/constant"
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
	return nil
}
