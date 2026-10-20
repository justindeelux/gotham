// JUS-250: the supplied logo asset, the host welcome banner, the named theme
// persistence and the token-backed xterm palette. The banner tests assert the
// EXACT bytes written to xterm (including wide vs narrow) and the asset hash,
// because a trimmed logo or an injected escape sequence is a real regression.
/* global StorageEvent:readonly, localStorage:readonly, window:readonly */
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";

import {
  DEFAULT_TERMINAL_APPEARANCE,
  DEFAULT_TERMINAL_THEME,
  TERMINAL_APPEARANCE_VERSION,
  parseTerminalAppearance,
} from "@/features/terminal/schemas/terminalAppearance";
import { useTerminalAppearanceStore } from "@/features/terminal/stores/terminalAppearance";
import {
  TERMINAL_LOGO,
  TERMINAL_LOGO_COLUMNS,
  TERMINAL_LOGO_DISPLAY,
  TERMINAL_LOGO_MIN_COLS,
  TERMINAL_LOGO_MIN_ROWS,
  TERMINAL_LOGO_SHA256,
  buildTerminalBanner,
  isHostTerminalTarget,
  sanitizeTerminalText,
} from "@/features/terminal/utils/terminalBanner";
import type { TerminalBannerInfo } from "@/features/terminal/utils/terminalBanner";
import {
  TERMINAL_APPEARANCE_MAX_BYTES,
  readTerminalAppearance,
  terminalAppearanceStorageKey,
  writeTerminalAppearance,
} from "@/features/terminal/utils/terminalAppearance";
import {
  terminalTheme,
  terminalThemeTokenPrefix,
} from "@/features/terminal/utils/terminalTheme";

const testsDir = dirname(fileURLToPath(import.meta.url));
const logoPath = resolve(
  testsDir,
  "../src/features/terminal/assets/gotham-ascii-logo.txt",
);
const tokensPath = resolve(testsDir, "../src/shared/styles/tokens.css");
const repoRoot = resolve(testsDir, "../..");

/** bannerInfo builds one host banner input. */
function bannerInfo(
  overrides: Partial<TerminalBannerInfo> = {},
): TerminalBannerInfo {
  return {
    version: "v0.2.1-dev",
    serverName: "prod-01",
    serverId: "server-1",
    target: "host",
    ...overrides,
  };
}

/** memoryStorage is a small Storage stand-in that can fail on demand. */
function memoryStorage(initial: Record<string, string> = {}) {
  const data = new Map(Object.entries(initial));
  return {
    getItem(key: string): string | null {
      return data.get(key) ?? null;
    },
    setItem(key: string, value: string): void {
      data.set(key, value);
    },
    raw: data,
  };
}

beforeEach(() => {
  localStorage.clear();
  setActivePinia(createPinia());
});

describe("Gotham logo asset", () => {
  it("is byte-for-byte the supplied file", () => {
    const bytes = readFileSync(logoPath);
    expect(createHash("sha256").update(bytes).digest("hex")).toBe(
      TERMINAL_LOGO_SHA256,
    );
    expect(TERMINAL_LOGO_SHA256).toBe(
      "76a2f2e19ef0f58326c1f79c5ac837cc25df96f40b7a6d606d292f27b177a274",
    );
    // The ?raw import must preserve Unicode blocks, trailing spaces and the
    // final line's absent newline.
    expect(TERMINAL_LOGO).toBe(bytes.toString("utf8"));
    expect(TERMINAL_LOGO.endsWith("\n")).toBe(false);
    expect(TERMINAL_LOGO.split("\n")).toHaveLength(19);
    expect(TERMINAL_LOGO).toContain("▓");
    expect(TERMINAL_LOGO).toContain("░");
  });
});

describe("terminal banner", () => {
  it("measures the logo as 63 xterm cells", () => {
    expect(TERMINAL_LOGO_COLUMNS).toBe(63);
    expect(TERMINAL_LOGO_MIN_COLS).toBe(63);
  });

  it("shows the full logo at every real renderer width from 63 up", () => {
    for (const columns of [100, 80, 63]) {
      const banner = buildTerminalBanner(
        bannerInfo(),
        columns,
        TERMINAL_LOGO_MIN_ROWS,
      );
      expect(banner.startsWith(TERMINAL_LOGO_DISPLAY)).toBe(true);
      expect(banner).toContain("Gotham v0.2.1-dev");
      expect(banner).toContain("server: prod-01");
      expect(banner.endsWith("\r\n\r\n")).toBe(true);
      for (const line of banner.split(/\r?\n/)) {
        expect([...line].length).toBeLessThanOrEqual(columns);
      }
    }
  });

  it("falls back to a compact banner at 62 and 40 columns", () => {
    for (const columns of [62, 40]) {
      const banner = buildTerminalBanner(bannerInfo(), columns, 30);
      expect(banner).not.toContain("▓");
      const lines = banner.split(/\r?\n/);
      for (const line of lines) {
        expect([...line].length).toBeLessThanOrEqual(columns);
      }
    }
    expect(buildTerminalBanner(bannerInfo(), 40, 30)).toBe(
      "Gotham v0.2.1-dev\r\nserver: prod-01\r\n\r\n",
    );
  });

  it("uses the compact banner when rows cannot fit the logo and a prompt", () => {
    const short = buildTerminalBanner(
      bannerInfo(),
      TERMINAL_LOGO_MIN_COLS,
      TERMINAL_LOGO_MIN_ROWS - 1,
    );
    expect(short).not.toContain("▓");
    const tall = buildTerminalBanner(
      bannerInfo(),
      TERMINAL_LOGO_MIN_COLS,
      TERMINAL_LOGO_MIN_ROWS,
    );
    expect(tall.startsWith(TERMINAL_LOGO_DISPLAY)).toBe(true);
  });

  it("keeps a long server name inside the pane", () => {
    const banner = buildTerminalBanner(
      bannerInfo({ serverName: "a-very-long-edge-server-name-01" }),
      24,
      10,
    );
    for (const line of banner.split(/\r?\n/)) {
      expect([...line].length).toBeLessThanOrEqual(24);
    }
    expect(banner).toContain("server: ");
  });

  it("localizes the server label without translating the brand", () => {
    const banner = buildTerminalBanner(
      bannerInfo({ serverLabel: "máy chủ" }),
      40,
      10,
    );
    expect(banner).toBe("Gotham v0.2.1-dev\r\nmáy chủ: prod-01\r\n\r\n");
  });

  it("omits the version when metadata is unknown", () => {
    const banner = buildTerminalBanner(bannerInfo({ version: null }), 42, 10);
    expect(banner).toBe("Gotham\r\nserver: prod-01\r\n\r\n");
    expect(banner).not.toMatch(/null|undefined/);
  });

  it("strips control sequences from every dynamic value on BOTH paths", () => {
    const hostile = "1.0\u001b[2J\u001b]0;PWN\u0007";
    for (const columns of [100, 42]) {
      const banner = buildTerminalBanner(
        bannerInfo({
          version: hostile,
          serverName: "\u001b[2Jprod\n01\u009b\u009d",
        }),
        columns,
        30,
      );
      expect(banner).not.toContain("\u001b");
      expect(banner).not.toContain("]0;PWN");
      expect(banner).not.toContain("\n01");
      expect(banner).not.toContain("\u009b");
      expect(banner).toContain("Gotham v1.0");
      expect(banner).toContain("prod 01");
    }
  });

  it("strips bidi overrides so a server name cannot reorder the banner", () => {
    const banner = buildTerminalBanner(
      bannerInfo({ serverName: "B\u202Eevil\u2066x\u200F\u061C" }),
      40,
      10,
    );
    expect(banner).toBe("Gotham v0.2.1-dev\r\nserver: Bevilx\r\n\r\n");
    expect(banner).not.toMatch(/[\u061c\u200e\u200f\u202a-\u202e\u2066-\u2069]/);
  });

  it("treats only host (or omitted) targets as host sessions", () => {
    expect(isHostTerminalTarget("host")).toBe(true);
    expect(isHostTerminalTarget("")).toBe(true);
    expect(isHostTerminalTarget("container:web-1")).toBe(false);
  });

  it("bounds sanitizeTerminalText by the requested width", () => {
    expect(sanitizeTerminalText("  a \n b  ", 20)).toBe("a b");
    expect(sanitizeTerminalText("abcdefghij", 5)).toBe("abcd…");
    expect(sanitizeTerminalText("\u001b]0;PWN\u0007visible", 40)).toBe(
      "visible",
    );
  });
});

describe("terminal banner asset protection", () => {
  it("pins the logo against editor and git normalisation", () => {
    const gitattributes = readFileSync(resolve(repoRoot, ".gitattributes"), "utf8");
    expect(gitattributes).toMatch(
      /web\/src\/features\/terminal\/assets\/gotham-ascii-logo\.txt -text/,
    );

    const editorconfig = readFileSync(resolve(repoRoot, ".editorconfig"), "utf8");
    const section = /\[web\/src\/features\/terminal\/assets\/\*\.txt\]([\s\S]*?)(?:\n\[|$)/.exec(
      editorconfig,
    );
    expect(section).not.toBeNull();
    expect(section?.[1]).toContain("trim_trailing_whitespace = false");
    expect(section?.[1]).toContain("insert_final_newline = false");
  });
});

describe("terminal appearance storage", () => {
  it("scopes the key to the signed-in account", () => {
    expect(terminalAppearanceStorageKey("u-1")).toBe(
      "gotham.terminal.appearance.v1.u-1",
    );
    expect(terminalAppearanceStorageKey("u/1")).toBe(
      "gotham.terminal.appearance.v1.u%2F1",
    );
    expect(terminalAppearanceStorageKey("")).toBe("");
  });

  it("round-trips a chosen theme and ignores corrupt or foreign payloads", () => {
    const storage = memoryStorage();
    expect(readTerminalAppearance(storage, "u-1")).toEqual(
      DEFAULT_TERMINAL_APPEARANCE,
    );
    expect(writeTerminalAppearance(storage, "u-1", { theme: "gotham-light" })).toBe(
      true,
    );
    expect(readTerminalAppearance(storage, "u-1").theme).toBe("gotham-light");

    storage.setItem(
      terminalAppearanceStorageKey("u-2"),
      JSON.stringify({ version: 99, theme: "gotham-light" }),
    );
    expect(readTerminalAppearance(storage, "u-2").theme).toBe(
      DEFAULT_TERMINAL_THEME,
    );

    storage.setItem(terminalAppearanceStorageKey("u-3"), "{not json");
    expect(readTerminalAppearance(storage, "u-3").theme).toBe(
      DEFAULT_TERMINAL_THEME,
    );

    storage.setItem(
      terminalAppearanceStorageKey("u-4"),
      "x".repeat(TERMINAL_APPEARANCE_MAX_BYTES + 1),
    );
    expect(readTerminalAppearance(storage, "u-4").theme).toBe(
      DEFAULT_TERMINAL_THEME,
    );
  });

  it("returns defaults when storage itself throws", () => {
    const storage = {
      getItem(): string | null {
        throw new Error("private mode");
      },
      setItem(): void {
        throw new Error("quota");
      },
    };
    expect(readTerminalAppearance(storage, "u-1")).toEqual(
      DEFAULT_TERMINAL_APPEARANCE,
    );
    expect(writeTerminalAppearance(storage, "u-1", { theme: "gotham-light" })).toBe(
      false,
    );
  });

  it("parses only the known version and resets a bogus theme field", () => {
    expect(
      parseTerminalAppearance({
        version: TERMINAL_APPEARANCE_VERSION,
        theme: "neon",
      }).theme,
    ).toBe(DEFAULT_TERMINAL_THEME);
    expect(parseTerminalAppearance(null)).toEqual(DEFAULT_TERMINAL_APPEARANCE);
  });
});

describe("terminal appearance store", () => {
  it("persists per account and restores it on the next store instance", () => {
    localStorage.setItem(
      "gotham.auth.session",
      JSON.stringify({ user: { id: "u-1" }, accessToken: "a", refreshToken: "r" }),
    );
    setActivePinia(createPinia());
    const first = useTerminalAppearanceStore();
    expect(first.theme).toBe("gotham-dark");
    first.setTheme("gotham-light");
    expect(localStorage.getItem("gotham.terminal.appearance.v1.u-1")).toContain(
      '"theme":"gotham-light"',
    );

    setActivePinia(createPinia());
    expect(useTerminalAppearanceStore().theme).toBe("gotham-light");
  });

  it("reads defaults for a corrupt entry and rebinds on account change", () => {
    localStorage.setItem(
      "gotham.auth.session",
      JSON.stringify({ user: { id: "u-1" }, accessToken: "a", refreshToken: "r" }),
    );
    localStorage.setItem("gotham.terminal.appearance.v1.u-1", "corrupt");
    setActivePinia(createPinia());
    const store = useTerminalAppearanceStore();
    expect(store.theme).toBe("gotham-dark");

    store.reloadForUser("u-2");
    expect(store.theme).toBe("gotham-dark");
  });

  it("follows a theme written by another tab of the same account", () => {
    localStorage.setItem(
      "gotham.auth.session",
      JSON.stringify({ user: { id: "u-1" }, accessToken: "a", refreshToken: "r" }),
    );
    setActivePinia(createPinia());
    const store = useTerminalAppearanceStore();
    expect(store.theme).toBe("gotham-dark");

    localStorage.setItem(
      "gotham.terminal.appearance.v1.u-1",
      JSON.stringify({ version: TERMINAL_APPEARANCE_VERSION, theme: "gotham-light" }),
    );
    window.dispatchEvent(
      new StorageEvent("storage", {
        key: "gotham.terminal.appearance.v1.u-1",
      }),
    );
    expect(store.theme).toBe("gotham-light");
  });
});

describe("terminal theme tokens", () => {
  it("maps the dark and light token groups onto xterm's ITheme", () => {
    const tokens = new Map([
      ["--term-dark-bg", "#111111"],
      ["--term-dark-fg", "#eeeeee"],
      ["--term-dark-cursor", "#5865f2"],
      ["--term-dark-selection", "#333333"],
      ["--term-dark-ansi-black", "#010101"],
      ["--term-dark-ansi-bright-white", "#ffffff"],
      ["--term-light-bg", "#fafafa"],
      ["--term-light-fg", "#111111"],
      ["--term-light-ansi-blue", "#1d4ed8"],
    ]);
    const read = (name: string) => tokens.get(name);

    const dark = terminalTheme("gotham-dark", read);
    expect(dark.background).toBe("#111111");
    expect(dark.foreground).toBe("#eeeeee");
    expect(dark.cursor).toBe("#5865f2");
    expect(dark.selectionBackground).toBe("#333333");
    expect(dark.black).toBe("#010101");
    expect(dark.brightWhite).toBe("#ffffff");

    const light = terminalTheme("gotham-light", read);
    expect(light.background).toBe("#fafafa");
    expect(light.foreground).toBe("#111111");
    expect(light.blue).toBe("#1d4ed8");
    // Missing tokens are omitted, never replaced by a hard-coded literal.
    expect(light.red).toBeUndefined();
  });

  it("uses one token prefix per named theme", () => {
    expect(terminalThemeTokenPrefix("gotham-dark")).toBe("--term-dark-");
    expect(terminalThemeTokenPrefix("gotham-light")).toBe("--term-light-");
  });
});

/** readCssTokens parses every custom-property declaration in tokens.css. */
function readCssTokens(): Map<string, string> {
  const css = readFileSync(tokensPath, "utf8");
  const tokens = new Map<string, string>();
  for (const match of css.matchAll(/--([a-z0-9-]+)\s*:\s*([^;]+);/gi)) {
    tokens.set(`--${match[1]!}`, match[2]!.trim());
  }
  return tokens;
}

/** resolveToken follows one level of var() to a concrete color value. */
function resolveToken(tokens: Map<string, string>, name: string): string {
  const value = tokens.get(name);
  if (value === undefined) {
    throw new Error(`missing token ${name}`);
  }
  const alias = /^var\((--[^)]+)\)$/.exec(value);
  return alias ? resolveToken(tokens, alias[1]!) : value;
}

/** channelLuminance is the WCAG 2.1 relative-luminance channel step. */
function channelLuminance(channel: number): number {
  const unit = channel / 255;
  return unit <= 0.03928 ? unit / 12.92 : ((unit + 0.055) / 1.055) ** 2.4;
}

/** relativeLuminance computes WCAG relative luminance for a hex color. */
function relativeLuminance(hex: string): number {
  const value = hex.replace("#", "");
  const channels = [0, 2, 4].map((offset) =>
    channelLuminance(Number.parseInt(value.slice(offset, offset + 2), 16)),
  );
  return (
    0.2126 * channels[0]! + 0.7152 * channels[1]! + 0.0722 * channels[2]!
  );
}

/** contrastRatio computes the WCAG ratio between two hex colors. */
function contrastRatio(foreground: string, background: string): number {
  const [light, dark] = [
    relativeLuminance(foreground),
    relativeLuminance(background),
  ].sort((a, b) => b - a);
  return (light! + 0.05) / (dark! + 0.05);
}

/** ANSI_FIELDS are the 16 palette entries xterm renders. */
const ANSI_FIELDS = [
  "black",
  "red",
  "green",
  "yellow",
  "blue",
  "magenta",
  "cyan",
  "white",
  "bright-black",
  "bright-red",
  "bright-green",
  "bright-yellow",
  "bright-blue",
  "bright-magenta",
  "bright-cyan",
  "bright-white",
] as const;

describe("terminal palette contrast", () => {
  it("keeps every ANSI colour readable in both named themes", () => {
    const tokens = readCssTokens();
    for (const theme of ["dark", "light"] as const) {
      const background = resolveToken(tokens, `--term-${theme}-bg`);
      for (const field of ANSI_FIELDS) {
        const color = resolveToken(tokens, `--term-${theme}-ansi-${field}`);
        const ratio = contrastRatio(color, background);
        // True black can never reach 4.5:1 on a near-black surface; it must
        // still be a visible neutral (>= 3:1). Every other ANSI entry keeps
        // normal-text contrast (>= 4.5:1).
        const minimum = theme === "dark" && field === "black" ? 3 : 4.5;
        expect(
          ratio,
          `${theme} ${field} ${color} on ${background} = ${ratio.toFixed(2)}`,
        ).toBeGreaterThanOrEqual(minimum);
      }
    }
  });

  it("keeps both cursors visible against their surfaces", () => {
    const tokens = readCssTokens();
    for (const theme of ["dark", "light"] as const) {
      const background = resolveToken(tokens, `--term-${theme}-bg`);
      const cursor = resolveToken(tokens, `--term-${theme}-cursor`);
      expect(contrastRatio(cursor, background)).toBeGreaterThanOrEqual(3);
    }
  });
});
