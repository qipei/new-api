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
import i18n, { type BackendModule } from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

import { convertDetectedLanguage } from './languages'

type LocaleModule = { default: { translation: Record<string, string> } }

// CUSTOM: 语言包按需加载（fork 扩展）。七种语言合计约 3.9 MB（压缩后约 1 MB），
// 原先全部打进首屏主包，每个页面都要先下载完才能渲染。现在只加载当前语言和回退语言
// （en），其余语言在切换时才下载；切换语言的调用都会等 changeLanguage 完成再刷新界面。
const localeLoaders: Record<string, () => Promise<LocaleModule>> = {
  en: () => import('./locales/en.json'),
  zhCN: () => import('./locales/zh.json'),
  fr: () => import('./locales/fr.json'),
  ru: () => import('./locales/ru.json'),
  ja: () => import('./locales/ja.json'),
  vi: () => import('./locales/vi.json'),
  zhTW: () => import('./locales/zh-TW.json'),
}

const localeBackend: BackendModule = {
  type: 'backend',
  init: () => {},
  // i18next 对只声明 (language, namespace) 两个参数的 read 按返回的 Promise 取结果，
  // 参数个数是它判断的依据，不能删掉第二个参数。
  read: async (language: string, _namespace: string) => {
    const load = localeLoaders[language]
    if (!load) return {}
    const module = await load()
    return module.default.translation
  },
}

// 首次渲染要等当前语言包加载完成，否则界面会先闪一下未翻译的英文 key（见 main.tsx）。
export const i18nReady = i18n
  .use(localeBackend)
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    fallbackLng: 'en',
    supportedLngs: ['en', 'zhCN', 'fr', 'ru', 'ja', 'vi', 'zhTW'],
    load: 'currentOnly',
    nsSeparator: false, // Allow literal colons in keys (e.g., URLs, labels)
    debug: import.meta.env.DEV,
    interpolation: {
      escapeValue: false, // not needed for react as it escapes by default
    },
    react: {
      // 语言包在首次渲染前已就绪，切换语言时 changeLanguage 也会先加载完，不需要 Suspense。
      useSuspense: false,
    },
    detection: {
      order: ['localStorage', 'navigator'],
      caches: ['localStorage'],
      // Browsers report `zh-CN`/`zh-TW`/`zh`; map them onto our `zhCN`/`zhTW`
      // codes (non-Chinese codes pass through for normal supportedLngs matching).
      convertDetectedLanguage,
    },
  })

export default i18n
