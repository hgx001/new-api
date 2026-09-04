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
  return path.replace(/\{model\}/g, modelName)
}

/**
 * Check if model is token-based pricing
 */
export function isTokenBasedModel(model: PricingModel): boolean {
  return model.quota_type === QUOTA_TYPE_VALUES.TOKEN
}

export function isPerSecondModel(model: PricingModel): boolean {
  return model.supported_endpoint_types?.includes('openai-video') ?? false
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
  'autodl:h3-video':
    'Text-to-video, no reference image needed. Duration 1-15s; 480p/768p in vertical, horizontal and 1:1. Billed per second.',
  'autodl:multiref-video-1':
    'Multi-reference video, 1-9 reference images required. Duration 1-10s; 480p/768p in vertical, horizontal and 1:1. Supports seed. Billed per second.',
  'autodl:multiref-video-2':
    'Multi-reference video, 15s version, 1-9 reference images required. Duration 1-15s; 480p/768p in vertical, horizontal and 1:1. Supports seed. Billed per second.',
  'autodl:multiref-video-3':
    'Multi-reference video, 12s version, 1-9 reference images required. Duration 1-12s; 736p only in vertical, horizontal and 1:1. Supports seed. Billed per second.',
}

/**
 * Get the translated fallback description key for a model, or null when the
 * model has no curated fallback. Callers should prefer backend data:
 * `model.description || (fallback && t(fallback)) || model.vendor_description`.
 */
export function getModelDescriptionKey(model: PricingModel): string | null {
  return MODEL_DESCRIPTION_KEYS[model.model_name] || null
}
