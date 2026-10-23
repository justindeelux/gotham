import { describe, expect, it } from "vitest";

import {
  detectLanguage,
  editorLanguages,
  languageLabel,
  languageName,
} from "@/features/files/utils/editor";
import type { EditorLanguageId } from "@/features/files/utils/editor";

describe("detectLanguage", () => {
  it.each<[string, EditorLanguageId]>([
    ["Dockerfile", "dockerfile"],
    ["Dockerfile.prod", "dockerfile"],
    ["app.Dockerfile", "dockerfile"],
    ["Makefile", "makefile"],
    ["GNUmakefile", "makefile"],
    ["Makefile.am", "makefile"],
    [".htaccess", "apache"],
    [".htpasswd", "apache"],
    [".env", "ini"],
    [".env.local", "ini"],
    ["dev.env", "ini"],
    [".editorconfig", "ini"],
    ["nginx.conf", "nginx"],
    ["site.nginx.conf", "nginx"],
    ["httpd.conf", "apache"],
    ["apache2.conf", "apache"],
    ["Gemfile", "ruby"],
    ["Rakefile", "ruby"],
    ["Guardfile", "ruby"],
    ["docker-compose.yml", "yaml"],
    ["etc/nginx.conf", "nginx"],
    ["config/.env", "ini"],
    ["DOCKERFILE", "dockerfile"],
    ["APP.PHP", "php"],
  ])("resolves %s by name", (path, language) => {
    expect(detectLanguage(path)).toBe(language);
  });

  it.each<[string, EditorLanguageId]>([
    ["index.php", "php"],
    ["legacy.php5", "php"],
    ["template.phtml", "php"],
    ["query.sql", "sql"],
    ["feed.xml", "xml"],
    ["diagram.svg", "xml"],
    ["main.rs", "rust"],
    ["Main.java", "java"],
    ["lib.c", "cpp"],
    ["lib.h", "cpp"],
    ["app.cpp", "cpp"],
    ["app.hpp", "cpp"],
    ["sketch.ino", "cpp"],
    ["init.lua", "lua"],
    ["app.rb", "ruby"],
    ["app.gemspec", "ruby"],
    ["vhost.conf", "apache"],
    ["php.ini", "ini"],
    ["app.properties", "ini"],
    ["site.cfg", "ini"],
    ["App.vue", "vue"],
    ["app.scss", "scss"],
    ["app.less", "less"],
    ["change.diff", "diff"],
    ["change.patch", "diff"],
    ["server.log", "log"],
    ["rules.mk", "makefile"],
    ["notes.txt", "plain"],
    ["config.toml", "toml"],
    ["main.go", "go"],
    ["page.html", "html"],
    ["styles.css", "css"],
    ["data.jsonc", "json"],
    ["data.yaml", "yaml"],
  ])("resolves %s by extension", (path, language) => {
    expect(detectLanguage(path)).toBe(language);
  });

  it.each([
    "README",
    "LICENSE",
    ".gitignore",
    "archive.unknownext",
    "",
  ])("falls back to plain text for %s", (path) => {
    expect(detectLanguage(path)).toBe("plain");
  });
});

describe("language catalogue", () => {
  it("lists every language once", () => {
    const ids = editorLanguages.map((language) => language.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("has a picker name and a status tag for every language", () => {
    for (const language of editorLanguages) {
      expect(languageLabel(language.id)).toBe(language.label);
      expect(languageName(language.id)).toBe(language.name);
    }
  });
});
