// Files footer pluralisation and the bulk-count reserve sizer (JUS-141 fix
// round 1): the breakdown counts read "1 folder" / "2 folders" in en and vi,
// and `widestBytesLabel` bounds a unit-boundary subset label (formatBytes is
// non-monotonic: "1023.0 KiB" is wider than "1.00 MiB").
import { beforeEach, describe, expect, it } from "vitest";

import { widestBytesLabel } from "@/features/files/utils/files";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";
import { formatBytes } from "@/shared/utils/format";

describe("widestBytesLabel", () => {
  it("is at least as wide as any subset label", () => {
    const cases: Array<[total: number, subset: number]> = [
      [1048576, 1047552], // "1.00 MiB" total, "1023.0 KiB" subset
      [1048576, 1024],
      [1048576, 1],
      [14745, 14000],
      [500, 1],
      [0, 0],
      [1024, 1024],
      [1073741824, 1048575], // "1.00 GiB" total, "1023.9 MiB" subset
    ];
    for (const [total, subset] of cases) {
      expect(widestBytesLabel(total).length).toBeGreaterThanOrEqual(
        formatBytes(subset).length,
      );
    }
  });

  it("picks the widest reachable label", () => {
    expect(widestBytesLabel(1048576)).toBe("1024.0 KiB");
    expect(widestBytesLabel(1047552)).toBe("1023.0 KiB");
    expect(widestBytesLabel(1024)).toBe("1.00 KiB");
    expect(widestBytesLabel(0)).toBe("0 B");
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
