import { ja } from "./ja";
import { en } from "./en";

export type LangCode = "ja" | "en";
export type LangSetting = "auto" | LangCode;

const dictionaries = { ja, en } as const;

function detectSystemLang(): LangCode {
  if (typeof navigator !== "undefined" && navigator.language) {
    return navigator.language.toLowerCase().startsWith("ja") ? "ja" : "en";
  }
  return "en";
}

export const i18nState = $state({
  setting: "auto" as LangSetting,
  lang: detectSystemLang() as LangCode,
});

export function applyLanguageSetting(setting?: string): void {
  const s: LangSetting = setting === "ja" || setting === "en" ? setting : "auto";
  i18nState.setting = s;
  i18nState.lang = s === "auto" ? detectSystemLang() : s;
}

export function t(path: string, params?: Record<string, string | number | undefined>): string {
  const dict: any = dictionaries[i18nState.lang] ?? dictionaries.en;
  const parts = path.split(".");
  let cur: any = dict;
  for (const p of parts) {
    if (cur && typeof cur === "object" && p in cur) {
      cur = cur[p];
    } else {
      cur = undefined;
      break;
    }
  }
  if (typeof cur !== "string") {
    return path;
  }
  if (!params) return cur;
  return cur.replace(/\{(\w+)\}/g, (_, k) => (params[k] !== undefined ? String(params[k]) : `{${k}}`));
}

export function locDisplayName(loc: { name: string; nameEn?: string }): string {
  if (i18nState.lang === "en") {
    return loc.nameEn || loc.name;
  }
  return loc.name || loc.nameEn || "";
}
