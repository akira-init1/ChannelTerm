import test from "node:test";
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import {
  mkdir,
  mkdtemp,
  readFile,
  writeFile,
  copyFile,
  rm,
} from "node:fs/promises";
import { join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { setupI18n } from "@lingui/core";
import { formatter } from "@lingui/format-po";
import { locales } from "../src/shared/menus";

test("official extraction preserves translations, adds messages/locales, and enforces strict compilation with English dev fallback", async () => {
  await mkdir("test-results", { recursive: true });
  const directory = await mkdtemp(resolve("test-results", "i18n-"));
  const config = join(directory, "lingui.config.ts");
  const cli = fileURLToPath(
    new URL("lingui.js", import.meta.resolve("@lingui/cli")),
  );
  const po = formatter({ lineNumbers: false });
  const allLocales = [...locales, "fr"];
  async function run(command: string, ...args: string[]) {
    return promisify(execFile)(
      process.execPath,
      [cli, command, "--config", config, "--workers", "1", ...args],
      { windowsHide: true },
    );
  }
  async function catalog(locale: string) {
    const filename = join(directory, "locales", locale, "messages.po");
    return po.parse(await readFile(filename, "utf8"), {
      locale,
      sourceLocale: "en",
      filename,
    });
  }
  try {
    await mkdir(join(directory, "source"));
    const source = join(directory, "source", "menu-messages.ts");
    await copyFile("src/shared/menu-messages.ts", source);
    const original = new Map<string, Awaited<ReturnType<typeof catalog>>>();
    for (const locale of locales) {
      const target = join(directory, "locales", locale);
      await mkdir(target, { recursive: true });
      await copyFile(
        `src/locales/${locale}/messages.po`,
        join(target, "messages.po"),
      );
      original.set(locale, await catalog(locale));
    }
    // Use the real project config/formatter, overriding only disposable paths
    // and adding a fourth locale. Production source/catalogs are never modified.
    await writeFile(
      config,
      `import base from ${JSON.stringify(resolve("lingui.config.ts").replaceAll("\\", "/"))};
export default { ...base, locales: ${JSON.stringify(allLocales)}, catalogs: [{ path: "<rootDir>/locales/{locale}/messages", include: ["<rootDir>/source"] }] };
`,
    );
    await writeFile(
      source,
      (await readFile(source, "utf8")) +
        '\nexport const probe = /* i18n */ { id: "test.catalog.probe", message: "New message {name}" };\n',
    );
    const extracted = await run("extract", "--clean");
    assert.doesNotMatch(
      extracted.stdout + extracted.stderr,
      /Missing message ID|Could not extract/,
    );
    for (const locale of allLocales) {
      const messages = await catalog(locale);
      assert.ok(Object.hasOwn(messages, "test.catalog.probe"));
      assert.equal(
        messages["test.catalog.probe"]?.translation,
        locale === "en" ? "New message {name}" : "",
      );
      for (const [id, entry] of Object.entries(original.get(locale) ?? {}))
        assert.equal(
          messages[id]?.translation,
          entry.translation,
          `${locale}: ${id}`,
        );
    }
    // The official compiler must reject missing translations rather than hiding
    // them behind an explicit fallbackLocales setting. Development may preview
    // missing text using Lingui's sourceLocale fallback.
    await assert.rejects(run("compile", "--strict"), /Missing|missing/);
    await run("compile");
    const generated = await import(
      pathToFileURL(join(directory, "locales/fr/messages.ts")).href
    );
    const i18n = setupI18n({
      locale: "fr",
      messages: { fr: generated.messages },
    });
    assert.equal(
      i18n._("test.catalog.probe", { name: "reader" }),
      "New message reader",
    );

    const english = await catalog("en");
    for (const locale of allLocales) {
      const filename = join(directory, "locales", locale, "messages.po");
      const messages = await catalog(locale);
      for (const [id, entry] of Object.entries(messages))
        if (!entry.translation)
          entry.translation = `Translated: ${english[id]?.translation}`;
      await writeFile(
        filename,
        await po.serialize(messages, {
          locale,
          sourceLocale: "en",
          filename,
          existing: await readFile(filename, "utf8"),
        }),
      );
    }
    await run("extract", "--clean");
    await run("compile", "--strict");
    assert.equal(
      (await catalog("ja"))["test.catalog.probe"]?.translation,
      "Translated: New message {name}",
    );
    assert.equal(
      (await catalog("fr"))["menu.connection"]?.translation,
      "Translated: Connection",
    );
  } finally {
    // mkdtemp created this exact directory below ignored test-results; never
    // remove caller-supplied paths or alter the real PO catalogs during tests.
    await rm(directory, { recursive: true, force: true });
  }
});
