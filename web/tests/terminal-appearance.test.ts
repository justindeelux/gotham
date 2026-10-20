// JUS-250: the supplied logo asset, the host welcome banner, the named theme
// persistence and the token-backed xterm palette. The banner tests assert the
// EXACT bytes written to xterm (including wide vs narrow) and the asset hash,
// because a trimmed logo or an injected escape sequence is a real regression.
/* global localStorage:readonly */
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
  TERMINAL_LOGO_MIN_COLS,
  TERMINAL_LOGO_SHA256,
  buildTerminalBanner,
  isHostTerminalTarget,
  sanitizeTerminalText,
} from "@/features/terminal/utils/terminalBanner";
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

/** bannerInfo builds one host banner input. */
function bannerInfo(overrides: Record<string, unknown> = {}) {
  return {
    version: "v0.2.1-dev",
    serverName: "prod-01",
    serverId: "server-1",
    target: "host",
    ...overrides,
  } as Parameters<typeof buildTerminalBanner>[0];
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
  it("shows the complete logo once for a wide host renderer", () => {
    const banner = buildTerminalBanner(bannerInfo(), TERMINAL_LOGO_MIN_COLS);
    expect(banner.startsWith(TERMINAL_LOGO)).toBe(true);
    expect(banner).toContain("Gotham v0.2.1-dev");
    expect(banner).toContain("server: prod-01");
    expect(banner.endsWith("\r\n\r\n")).toBe(true);
  });

  it("falls back to a compact banner on narrow terminals", () => {
    const banner = buildTerminalBanner(bannerInfo(), 42);
    expect(banner).not.toContain("▓");
    expect(banner).toBe("Gotham v0.2.1-dev\r\nserver: prod-01\r\n\r\n");
    for (const line of banner.split("\r\n")) {
      expect(line.length).toBeLessThanOrEqual(42);
    }
  });

  it("keeps a long server name inside the pane", () => {
    const banner = buildTerminalBanner(
      bannerInfo({ serverName: "a-very-long-edge-server-name-01" }),
      24,
    );
    for (const line of banner.split("\r\n")) {
      expect(line.length).toBeLessThanOrEqual(24);
    }
    expect(banner).toContain("server: ");
  });

  it("omits the version when metadata is unknown", () => {
    const banner = buildTerminalBanner(bannerInfo({ version: null }), 42);
    expect(banner).toBe("Gotham\r\nserver: prod-01\r\n\r\n");
    expect(banner).not.toMatch(/null|undefined/);
  });

  it("strips control sequences from metadata before writing", () => {
    const banner = buildTerminalBanner(
      bannerInfo({
        version: "v0.2.1\u001b[31m",
        serverName: "\u001b[2Jprod\n01",
      }),
      42,
    );
    expect(banner).not.toContain("\u001b");
    expect(banner).not.toContain("\n01");
    expect(banner).toBe("Gotham v0.2.1\r\nserver: prod 01\r\n\r\n");
  });

  it("treats only host (or omitted) targets as host sessions", () => {
    expect(isHostTerminalTarget("host")).toBe(true);
    expect(isHostTerminalTarget("")).toBe(true);
    expect(isHostTerminalTarget("container:web-1")).toBe(false);
  });

  it("bounds sanitizeTerminalText by the requested width", () => {
    expect(sanitizeTerminalText("  a \n b  ", 20)).toBe("a b");
    expect(sanitizeTerminalText("abcdefghij", 5)).toBe("abcd…");
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
