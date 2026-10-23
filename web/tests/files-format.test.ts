import { beforeEach, describe, expect, it } from "vitest";

import { ownerModeLabel, widestBytesLabel } from "@/features/files/utils/files";
import type { FilesEntry } from "@/features/files/schemas/files";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";
import { formatBytes } from "@/shared/utils/format";

describe("widestBytesLabel", () => {
  it("bounds every label in [0, total] across the rounding bands", () => {
    const limit = 1_050_000;
    let widestSoFar = 0;
    const failures: number[] = [];
    for (let value = 0; value <= limit; value += 1) {
      widestSoFar = Math.max(widestSoFar, formatBytes(value).length);
      if (widestBytesLabel(value).length < widestSoFar) {
        failures.push(value);
      }
    }
    expect(failures.slice(0, 5)).toEqual([]);
  });

  it("bounds the named boundary cases", () => {
    const cases: Array<[total: number, subset: number]> = [
      [102400, 102399],
      [102400, 1],
      [102399, 102398],
      [1048576, 1047552],
      [1048576, 1024],
      [1048576, 1],
      [14745, 14000],
      [500, 1],
      [0, 0],
      [1024, 1024],
      [1073741824, 1048575],
    ];
    for (const [total, subset] of cases) {
      expect(
        widestBytesLabel(total).length,
        `total ${total} vs subset ${subset}`,
      ).toBeGreaterThanOrEqual(formatBytes(subset).length);
    }
  });

  it("covers the [99.995, 100) x unit bands for B/KiB/MiB/GiB", () => {
    for (const base of [1, 1024, 1024 ** 2, 1024 ** 3]) {
      const peak = base * 100 - 1;
      const total = base * 100;
      expect(widestBytesLabel(total).length).toBeGreaterThanOrEqual(
        formatBytes(peak).length,
      );
      expect(widestBytesLabel(base * 1000).length).toBeGreaterThanOrEqual(
        formatBytes(base * 1000 - 1).length,
      );
    }
  });

  it("picks a label at least as wide as every reachable one", () => {
    expect(widestBytesLabel(102400)).toBe("100.00 KiB");
    expect(widestBytesLabel(1048576).length).toBe(10);
    expect(widestBytesLabel(1024)).toBe("1.00 KiB");
    expect(widestBytesLabel(0)).toBe("0 B");
  });

  it("bounds the reserve summary for 0/1/5000 entries", () => {
    registerDiscoveredCatalogs();
    resetLocaleState();
    syncComposerLocale("en");
    const summary = (count: number, bytes: number): string =>
      `${i18n.global.t("files.browser.selectionCount", { count })} · ${formatBytes(bytes)}`;
    const folders = [
      { totalBytes: 102400, totalCount: 2, liveBytes: 102399, liveCount: 1 },
      { totalBytes: 1048576, totalCount: 5000, liveBytes: 1024, liveCount: 1 },
      { totalBytes: 0, totalCount: 0, liveBytes: 0, liveCount: 0 },
    ];
    for (const folder of folders) {
      const reserve = `${i18n.global.t("files.browser.selectionCount", { count: folder.totalCount })} · ${widestBytesLabel(folder.totalBytes)}`;
      const live = summary(folder.liveCount, folder.liveBytes);
      expect(reserve.length).toBeGreaterThanOrEqual(live.length);
    }
  });
});

describe("files footer plurals", () => {
  beforeEach(() => {
    registerDiscoveredCatalogs();
    resetLocaleState();
  });

  it("pluralizes the breakdown for 0/1/2 in en", () => {
    syncComposerLocale("en");
    const t = (key: string, count: number): string =>
      String(i18n.global.t(key, { count }));
    expect(t("files.browser.entryCount", 1)).toBe("1 entry");
    expect(t("files.browser.entryCount", 2)).toBe("2 entries");
    expect(t("files.browser.folderCount", 1)).toBe("1 folder");
    expect(t("files.browser.folderCount", 2)).toBe("2 folders");
    expect(t("files.browser.folderCount", 0)).toBe("0 folders");
    expect(t("files.browser.fileCount", 1)).toBe("1 file");
    expect(t("files.browser.fileCount", 2)).toBe("2 files");
    expect(t("files.browser.symlinkCount", 1)).toBe("1 symlink");
    expect(t("files.browser.symlinkCount", 2)).toBe("2 symlinks");
    expect(
      i18n.global.t("files.browser.entryCountFiltered", {
        count: 1,
        shown: 1,
        total: 1,
      }),
    ).toBe("1 of 1 entry");
    expect(
      i18n.global.t("files.browser.entryCountFiltered", {
        count: 2,
        shown: 1,
        total: 2,
      }),
    ).toBe("1 of 2 entries");
  });

  it("keeps the vi forms (identical segments) intact", () => {
    syncComposerLocale("vi");
    const t = (key: string, count: number): string =>
      String(i18n.global.t(key, { count }));
    expect(t("files.browser.folderCount", 1)).toBe("1 thư mục");
    expect(t("files.browser.folderCount", 2)).toBe("2 thư mục");
    expect(t("files.browser.fileCount", 2)).toBe("2 tệp");
    expect(t("files.browser.symlinkCount", 2)).toBe("2 liên kết");
    expect(t("files.browser.entryCount", 1)).toBe("1 mục");
  });
});

describe("ownerModeLabel", () => {
  const entry = (overrides: Partial<FilesEntry>): FilesEntry => ({
    name: "f",
    kind: "file",
    size: 0,
    mode: 0o100755,
    modified_at: "2026-10-12T09:02:00Z",
    etag: '"1"',
    ...overrides,
  });

  it("joins the owner and the octal mode", () => {
    expect(ownerModeLabel(entry({ owner: "root" }))).toBe("root / 755");
  });

  it("keeps a special-bits digit", () => {
    expect(ownerModeLabel(entry({ owner: "root", mode: 0o104755 }))).toBe(
      "root / 4755",
    );
  });

  it("renders the mode alone when the owner is absent", () => {
    expect(ownerModeLabel(entry({}))).toBe("755");
  });
});
