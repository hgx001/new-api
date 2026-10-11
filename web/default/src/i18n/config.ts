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
import i18n, { type ResourceLanguage } from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

const SUPPORTED_LANGUAGES = ['en', 'zh', 'fr', 'ru', 'ja', 'vi']

type SupportedLanguage = (typeof SUPPORTED_LANGUAGES)[number]

type LocaleBundle = Record<string, unknown>

// Each locale file is 330-500 KB, so statically importing all six pushed every
// translation into the entry chunk and made each visitor download ~2.4 MB of
// JSON for languages they never see. Each locale is code-split here instead and
// fetched only when it is actually needed.
const LOCALE_LOADERS: Record<
  SupportedLanguage,
  () => Promise<{ default?: LocaleBundle }>
> = {
  en: () => import('./locales/en.json'),
  zh: () => import('./locales/zh.json'),
  fr: () => import('./locales/fr.json'),
  ru: () => import('./locales/ru.json'),
  ja: () => import('./locales/ja.json'),
  vi: () => import('./locales/vi.json'),
}

function isSupportedLanguage(language: string): language is SupportedLanguage {
  return (SUPPORTED_LANGUAGES as string[]).includes(language)
}

const pendingLoads = new Map<string, Promise<void>>()

/**
 * Load a locale bundle and register it with i18next.
 *
 * Concurrent calls for the same language share one in-flight promise, and the
 * resolved bundle is cached by i18next's own store, so switching back and forth
 * never re-downloads a locale.
 */
export function loadLanguage(language: string): Promise<void> {
  const target = isSupportedLanguage(language) ? language : 'en'

  const existing = pendingLoads.get(target)
  if (existing) {
    return existing
  }

  if (i18n.hasResourceBundle(target, 'translation')) {
    return Promise.resolve()
  }

  const promise = LOCALE_LOADERS[target]().then((module) => {
    // rspack's JSON loader returns the object directly, while a strict ESM
    // interop build exposes it as the module's `default` export.
    const bundle = module.default ?? (module as LocaleBundle)
    // Each locale file nests its strings under a `translation` namespace, so
    // register that inner object for the default namespace. Passing the whole
    // file instead would nest the strings one level too deep (translation.
    // translation.Sign in) and every lookup would silently miss.
    const namespace = (bundle.translation ?? bundle) as ResourceLanguage
    i18n.addResourceBundle(target, 'translation', namespace, true, true)
  })

  pendingLoads.set(target, promise)
  return promise
}

i18n.use(LanguageDetector).use(initReactI18next)

// `partialBundledLanguages` is what allows resources to be registered after
// init via addResourceBundle; without it i18next treats an empty `resources`
// object as "already loaded" and never asks again.
i18n.init({
  partialBundledLanguages: true,
  resources: {},
  fallbackLng: 'en',
  supportedLngs: SUPPORTED_LANGUAGES,
  load: 'languageOnly', // Convert zh-CN -> zh
  nsSeparator: false, // Allow literal colons in keys (e.g., URLs, labels)
  debug: import.meta.env.DEV,
  interpolation: {
    escapeValue: false, // not needed for react as it escapes by default
  },
  detection: {
    order: ['localStorage', 'navigator'],
    caches: ['localStorage'],
  },
})

/**
 * Resolves once the detected language's bundle is registered.
 *
 * The app awaits this before its first render so users never see raw
 * translation keys while the locale chunk is still downloading.
 */
export const i18nReady: Promise<void> = loadLanguage(
  i18n.resolvedLanguage ?? i18n.language
).then(async () => {
  // Registering a bundle does not notify react-i18next on its own; re-applying
  // the active language makes components render with the freshly added strings.
  await i18n.changeLanguage(i18n.resolvedLanguage ?? i18n.language)
})

export default i18n
