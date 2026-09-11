// Copyright (C) 2023-2026 QuantumNous
//
// This file is part of New API.
//
// New API is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package constant

import "strings"

var autoDLLegacyModelNames = map[string]string{
	// The first four names are the identifiers currently stored by the live
	// AutoDL channel. Keep the workflow identity while replacing positional
	// aliases with names that downstream callers can understand.
	"autodl:h3-video":           "autodl:minimax-h3-text-to-video",
	"autodl:multiref-video-1":   "autodl:minimax-h3-lightx2v-v5",
	"autodl:multiref-video-2":   "autodl:minimax-h3-lightx2v-v5-15s",
	"autodl:multiref-video-3":   "autodl:minimax-h3-b99-12s",
	"autodl:multiaudio-video-1": "autodl:minimax-h3-u24",
	"autodl:multiaudio-video-2": "autodl:minimax-h3-u08",
	"autodl:multiaudio-video-3": "autodl:minimax-h3-image-audio-10s",
	"autodl:multiaudio-video-4": "autodl:minimax-h3-image-audio-15s",
	"autodl:audio-sync-video":   "autodl:minimax-h3-lipsync",
}

// MigrateAutoDLModelNames converts the old AutoDL model identifiers to their
// descriptive names and returns whether the input required normalization.
func MigrateAutoDLModelNames(models []string) ([]string, bool) {
	migrated := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	changed := false
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			changed = true
			continue
		}
		if canonical, ok := autoDLLegacyModelNames[model]; ok {
			model = canonical
			changed = true
		}
		if _, ok := seen[model]; ok {
			changed = true
			continue
		}
		seen[model] = struct{}{}
		migrated = append(migrated, model)
	}
	return migrated, changed
}
