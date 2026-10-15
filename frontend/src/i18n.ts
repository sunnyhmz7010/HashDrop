/* 基于 HashPay src/app/i18n.ts 修改：使用本站语言偏好，存储不可用时仍支持当前页面切换。
 * Copyright 2026 TGDash Team，Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。
 */
import { computed, ref } from "vue";
import {
  messages,
  normalizeLocale,
  translate,
  type I18nParams,
  type Locale,
  type MessageKey,
} from "./locales";

const storageKey = "hashdrop.locale";
export const locale = ref<Locale>(initialLocale());
export const localeOptions = computed(() => [
  { label: t("locale.zh"), value: "zh-CN" },
  { label: t("locale.en"), value: "en-US" },
]);

export function useI18n() {
  return { locale, localeOptions, setLocale, t };
}

export function t(key: MessageKey | string, params?: I18nParams): string {
  return translate(locale.value, key, params);
}

export function setLocale(value: Locale | string) {
  locale.value = normalizeLocale(value);
  try {
    localStorage.setItem(storageKey, locale.value);
  } catch {
    console.warn("无法保存语言偏好，当前页面仍使用所选语言");
  }
}

function initialLocale(): Locale {
  try {
    const saved = localStorage.getItem(storageKey);
    if (saved) return normalizeLocale(saved);
  } catch {
    console.warn("无法读取语言偏好，使用浏览器语言");
  }
  return normalizeLocale(navigator.language);
}

// 保持后端现有错误字符串接口；已显示的错误也能在切换语言后重新翻译。
const errorKeys = new Map<string, MessageKey>();
for (const key of Object.keys(messages["zh-CN"]) as MessageKey[]) {
  if (!key.startsWith("errors.")) continue;
  errorKeys.set(messages["zh-CN"][key], key);
  errorKeys.set(messages["en-US"][key], key);
}
export function translateError(message: string): string {
  const key = errorKeys.get(message);
  return key ? t(key) : message;
}

export function formatTime(seconds: number): string {
  return new Date(seconds * 1000).toLocaleString(locale.value, {
    hour12: false,
  });
}
