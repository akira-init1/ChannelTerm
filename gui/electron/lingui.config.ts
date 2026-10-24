import { defineConfig } from "@lingui/conf";
import { formatter } from "@lingui/format-po";
import { locales } from "./src/shared/menus";

export default defineConfig({
  locales: [...locales],
  sourceLocale: "en",
  // In Lingui 5, explicit fallbackLocales can hide missing target translations
  // from --strict. Non-strict compilation still uses sourceLocale as a fallback.
  fallbackLocales: false,
  catalogs: [
    {
      path: "<rootDir>/src/locales/{locale}/messages",
      include: ["<rootDir>/src"],
    },
  ],
  format: formatter({ lineNumbers: false }),
  orderBy: "messageId",
  compileNamespace: "ts",
});
