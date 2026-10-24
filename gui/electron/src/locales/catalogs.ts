import type { Messages } from "@lingui/core";
import type { MenuLocale } from "../shared/menus";
import { messages as en } from "./en/messages";
import { messages as zhCN } from "./zh-CN/messages";
import { messages as ja } from "./ja/messages";

// Each process bundles its own copy of the official CLI output. No i18n instance,
// privileged API, filesystem loader, or credentials cross the process boundary.
export const menuCatalogs = { en, "zh-CN": zhCN, ja } satisfies Record<
  MenuLocale,
  Messages
>;
