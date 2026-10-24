import type { Messages } from "@lingui/core";
import type { MenuLocale } from "../shared/menus";
import { messages as en } from "./en/messages";
import { messages as de } from "./de/messages";
import { messages as es } from "./es/messages";
import { messages as fr } from "./fr/messages";
import { messages as ko } from "./ko/messages";
import { messages as ru } from "./ru/messages";
import { messages as zhTW } from "./zh-TW/messages";
import { messages as zhCN } from "./zh-CN/messages";
import { messages as ja } from "./ja/messages";

// Each process bundles its own copy of the official CLI output. No i18n instance,
// privileged API, filesystem loader, or credentials cross the process boundary.
export const menuCatalogs = {
  en,
  de,
  es,
  fr,
  ja,
  ko,
  ru,
  "zh-CN": zhCN,
  "zh-TW": zhTW,
} satisfies Record<MenuLocale, Messages>;
