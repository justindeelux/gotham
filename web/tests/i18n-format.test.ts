import { beforeEach, describe, expect, it, vi } from "vitest";

import { resetLocaleState, setLocale, syncComposerLocale } from "@/shared/i18n";
import { tickLabel } from "@/shared/ui/chartModel";
import {
  expiryLabel,
  formatBytes,
  formatDate,
  relativeTime,
} from "@/shared/utils/format";

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
  vi.useRealTimers();
});

describe("relativeTime", () => {
  it("covers past and future in English", () => {
    vi.setSystemTime(new Date("2026-10-06T12:00:00Z"));
    expect(relativeTime("2026-10-06T11:55:00Z")).toBe("5m ago");
    expect(relativeTime("2026-10-06T12:05:00Z")).toBe("in 5m");
    expect(relativeTime("2026-10-06T11:59:50Z")).toBe("just now");
    expect(relativeTime("2026-10-06T12:00:10Z")).toBe("in a moment");
  });

  it("covers past and future in Vietnamese", () => {
    vi.setSystemTime(new Date("2026-10-06T12:00:00Z"));
    setLocale("vi", null);
    expect(relativeTime("2026-10-06T11:55:00Z")).toBe("5m trước");
    expect(relativeTime("2026-10-06T12:05:00Z")).toBe("sau 5m");
    expect(relativeTime("2026-10-06T11:59:50Z")).toBe("vừa xong");
    expect(relativeTime("2026-10-06T12:00:10Z")).toBe("sắp tới");
  });

  it("handles missing and invalid timestamps in both locales", () => {
    expect(relativeTime(null)).toBe("never");
    expect(relativeTime("not-a-date")).toBe("unknown");
    setLocale("vi", null);
    expect(relativeTime(undefined)).toBe("không bao giờ");
    expect(relativeTime("not-a-date")).toBe("không rõ");
  });
});

describe("expiryLabel", () => {
  it("covers zero, one and many days in English", () => {
    vi.setSystemTime(new Date("2026-10-06T12:00:00Z"));
    expect(expiryLabel("2026-10-07T12:00:00Z")).toBe("expires in 1 day");
    expect(expiryLabel("2026-10-09T12:00:00Z")).toBe("expires in 3 days");
    expect(expiryLabel("2026-10-05T12:00:00Z")).toBe("expired 1 day ago");
    expect(expiryLabel("2026-10-03T12:00:00Z")).toBe("expired 3 days ago");
    expect(expiryLabel("2026-10-06T12:00:00Z")).toBe("expires today");
  });

  it("covers one and many days in Vietnamese", () => {
    vi.setSystemTime(new Date("2026-10-06T12:00:00Z"));
    setLocale("vi", null);
    expect(expiryLabel("2026-10-07T12:00:00Z")).toBe("hết hạn sau 1 ngày");
    expect(expiryLabel("2026-10-09T12:00:00Z")).toBe("hết hạn sau 3 ngày");
    expect(expiryLabel("2026-10-05T12:00:00Z")).toBe("đã hết hạn 1 ngày trước");
    expect(expiryLabel("2026-10-06T12:00:00Z")).toBe("hết hạn hôm nay");
  });

  it("handles missing and invalid timestamps", () => {
    expect(expiryLabel(null)).toBe("—");
    expect(expiryLabel("nope")).toBe("unknown");
    setLocale("vi", null);
    expect(expiryLabel("nope")).toBe("không rõ");
  });
});

describe("formatBytes", () => {
  it("keeps binary units in both locales", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1536)).toBe("1.50 KiB");
    expect(formatBytes(5 * 1024 ** 3)).toBe("5.00 GiB");
    setLocale("vi", null);
    expect(formatBytes(1536)).toBe("1.50 KiB");
    expect(formatBytes(null)).toBe("—");
    expect(formatBytes(Number.NaN)).toBe("—");
  });
});

describe("formatDate", () => {
  it("renders a short date per display locale and guards bad input", () => {
    const iso = "2026-09-28T00:00:00Z";
    const en = formatDate(iso);
    expect(en).toMatch(/28.*Sep.*2026/);
    setLocale("vi", null);
    expect(formatDate(iso)).not.toBe(en);
    expect(formatDate(null)).toBe("—");
    expect(formatDate("nope")).toBe("không rõ");
  });
});

describe("tickLabel", () => {
  it("renders axis labels in both locales", () => {
    const at = Date.UTC(2026, 0, 5, 14, 5);
    expect(tickLabel(at, 60_000)).toMatch(/\d{2}:\d{2}/);
    setLocale("vi", null);
    expect(tickLabel(at, 60_000)).toMatch(/\d{2}:\d{2}/);
    expect(tickLabel(Date.UTC(2026, 0, 5), 24 * 60 * 60_000)).not.toBe("");
  });
});
