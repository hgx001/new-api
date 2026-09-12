/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { EXCLUDED_GROUPS, QUOTA_TYPE_VALUES } from '../constants'
import type { PricingModel } from '../types'

// ----------------------------------------------------------------------------
// Model Helper Utilities
// ----------------------------------------------------------------------------

/**
 * Get available groups for a model
 */
export function getAvailableGroups(
  model: PricingModel,
  usableGroup: Record<string, { desc: string; ratio: number }>
): string[] {
  const modelEnableGroups = Array.isArray(model.enable_groups)
    ? model.enable_groups
    : []

  return Object.keys(usableGroup)
    .filter((g) => !EXCLUDED_GROUPS.includes(g))
    .filter((g) => modelEnableGroups.includes(g))
}

/**
 * Replace model placeholder in endpoint path
 */
export function replaceModelInPath(path: string, modelName: string): string {
  return path.replaceAll('{model}', modelName)
}

/**
 * Check if model is token-based pricing
 */
export function isTokenBasedModel(model: PricingModel): boolean {
  return model.quota_type === QUOTA_TYPE_VALUES.TOKEN
}

/**
 * Check if a video model is billed per second.
 *
 * Only adaptors whose EstimateBilling returns a `seconds` ratio bill per
 * second: wan3/youzanwan3 (wan3.0-video*) and autodl (autodl:* workflows).
 * Every other openai-video model (e.g. seedance* via doubao) is billed a
 * fixed price per call, so it must NOT show the per-second suffix.
 * If a new per-second adaptor (e.g. sora) goes live, add its model prefix here.
 */
export function isPerSecondModel(model: PricingModel): boolean {
  const name = model.model_name ?? ''
  return name.startsWith('wan3.0-video') || name.startsWith('autodl:')
}

// ----------------------------------------------------------------------------
// Display name overrides for the Model Square. The actual model_name used in
// API calls, URLs, and copy-to-clipboard behavior is left unchanged.
// ----------------------------------------------------------------------------
const MODEL_DISPLAY_NAME_OVERRIDES: Record<string, string> = {
  'kimi-for-coding': 'kimi-k2.7',
  // sd2.5 慢速满血版实际支持 30 秒（youkou 模型 id 仍保留 "20秒内" 以便路由）
  'sd2.5慢速20秒内排队满血版': 'sd2.5慢速30秒内排队满血版',
}

/** Channel namespace prefixes hidden from the user-facing display name. */
const HIDDEN_CHANNEL_PREFIXES = ['autodl:']

/**
 * Get the user-facing display name for a model. Falls back to the internal
 * model_name when no override is configured. Channel namespace prefixes
 * (e.g. `autodl:`) are stripped for readability; the raw model_name is
 * still used for API calls and copy-to-clipboard.
 */
export function getModelDisplayName(model: PricingModel): string {
  const override = MODEL_DISPLAY_NAME_OVERRIDES[model.model_name]
  if (override) return override
  for (const prefix of HIDDEN_CHANNEL_PREFIXES) {
    if (model.model_name.startsWith(prefix)) {
      return model.model_name.slice(prefix.length)
    }
  }
  return model.model_name
}

// ----------------------------------------------------------------------------
// Description fallbacks for the Model Square. Used only when the backend
// provides neither a model description nor a vendor description, so admin
// customizations in the models/vendors tables always take precedence.
// ----------------------------------------------------------------------------
const MODEL_DESCRIPTION_KEYS: Record<string, string> = {
  'autodl:minimax-h3-text-to-video':
    'Text-to-video, no reference image needed. Duration 1-15s; 480p/768p in vertical, horizontal and 1:1. Billed per second.',
  'autodl:minimax-h3-lightx2v-v5':
    'Multi-reference video, 1-9 reference images required. Duration 1-10s; 480p/768p in vertical, horizontal and 1:1. Supports seed. Billed per second.',
  'autodl:minimax-h3-lightx2v-v5-15s':
    'Multi-reference video, 15s version, 1-9 reference images required. Duration 1-15s; 480p/768p in vertical, horizontal and 1:1. Supports seed. Billed per second.',
  'autodl:minimax-h3-b99-12s':
    'Multi-reference video, 12s version, 1-9 reference images required. Duration 1-12s; 736p only in vertical, horizontal and 1:1. Supports seed. Billed per second.',
  'autodl:minimax-h3-u24':
    'Multi-reference video with audio, quality-priority variant. Requires 1-9 images and accepts up to 3 audio tracks; duration 1-15s; 480p/768p in vertical, horizontal and 1:1; seed accepts 0.',
  'autodl:minimax-h3-u08':
    'Multi-reference video with audio, speed-priority variant. Requires 1-9 images and accepts up to 3 audio tracks; duration 1-15s; 480p/768p in vertical, horizontal and 1:1; seed accepts 0.',
  'autodl:minimax-h3-image-audio-10s':
    'Multi-reference video with audio, 10-second variant. Images and audio are optional; up to 9 images and 3 audio tracks; duration 1-10s; 480p/768p/1080p in vertical and horizontal.',
  'autodl:minimax-h3-image-audio-15s':
    'Multi-reference video with audio, 15-second variant. Images and audio are optional; up to 9 images and 3 audio tracks; duration 1-15s; 480p/768p in vertical and horizontal; no 1080p.',
  'autodl:minimax-h3-lipsync':
    'Single-image audio-synchronized video with automatic lip sync. Requires exactly one image and one audio track; uses audio_duration for 1-15s; supports 480p/768p/1080p and has no prompt field.',
}

/**
 * i18n key for the H3 resolution tier note. ASCII-only on purpose: the
 * project convention is English source strings as keys, and non-ASCII keys
 * silently miss at runtime (t() falls back to the raw key).
 *
 * Tiers: 480p/736p base (¥0.10/s), 768p 720p-tier (×1.2 = ¥0.12/s),
 * 1080p ×2.0 = ¥0.20/s. Formula: ModelPrice × seconds × size ratio.
 */
export const H3_RESOLUTION_TIER_NOTE = 'h3ResolutionTierNote'

const H3_RESOLUTION_TIERED_MODELS = [
  'autodl:minimax-h3-text-to-video',
  'autodl:minimax-h3-lightx2v-v5',
  'autodl:minimax-h3-lightx2v-v5-15s',
  'autodl:minimax-h3-b99-12s',
]

/**
 * Names of the four H3 workflows that share the same resolution tier table.
 */
export function getResolutionTieredModels(): string[] {
  return [...H3_RESOLUTION_TIERED_MODELS]
}

/**
 * Per-model resolution tier note shown in the Model Square detail drawer.
 * Returns the note for the four H3 workflows, null for everything else so
 * callers can fall back to existing description rendering.
 */
export function getResolutionTierNoteKey(model: PricingModel): string | null {
  if (H3_RESOLUTION_TIERED_MODELS.includes(model.model_name ?? '')) {
    return H3_RESOLUTION_TIER_NOTE
  }
  return null
}

/**
 * Get the translated fallback description key for a model, or null when the
 * model has no curated fallback. Callers should prefer backend data:
 * `model.description || (fallback && t(fallback)) || model.vendor_description`.
 */
export function getModelDescriptionKey(model: PricingModel): string | null {
  return MODEL_DESCRIPTION_KEYS[model.model_name] || null
}

/**
 * Get concise capability badges for Model Square cards. These are deliberately
 * separate from the description so the key differences remain visible even
 * when an administrator has configured a custom model description.
 */
export function getModelHighlightKeys(model: PricingModel): string[] {
  switch (model.model_name) {
    case 'autodl:minimax-h3-u24':
      return [
        'Quality priority',
        '1-15s',
        '1-9 images required',
        'Up to 3 audio tracks',
        '1:1 supported',
        'Seed starts at 0',
      ]
    case 'autodl:minimax-h3-u08':
      return [
        'Speed priority',
        '1-15s',
        '1-9 images required',
        'Up to 3 audio tracks',
        '1:1 supported',
        'Seed starts at 0',
      ]
    case 'autodl:minimax-h3-image-audio-10s':
      return [
        'Images/audio optional',
        '1-10s',
        'Up to 9 images',
        'Up to 3 audio tracks',
        '1080p supported',
        'No 1:1',
      ]
    case 'autodl:minimax-h3-image-audio-15s':
      return [
        'Images/audio optional',
        '1-15s',
        'Up to 9 images',
        'Up to 3 audio tracks',
        'Up to 768p',
        'No 1080p',
      ]
    case 'autodl:minimax-h3-lipsync':
      return [
        'Auto lip sync',
        '1 image + 1 audio required',
        '1-15s via audio_duration',
        '1080p supported',
        'No prompt',
      ]
    default:
      return []
  }
}
