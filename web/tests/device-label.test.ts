// deviceLabel (JUS-28): user-agent sniffing stays readable and falls back
// to "Unknown device" for anything it does not recognize.
import { describe, expect, it } from "vitest";

import { deviceLabel } from "@/features/profile/utils/deviceLabel";

describe("deviceLabel", () => {
  it("names Chrome on macOS", () => {
    expect(
      deviceLabel(
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
          "(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
      ),
    ).toBe("Chrome on macOS");
  });

  it("names Firefox on Windows", () => {
    expect(
      deviceLabel(
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:127.0) Gecko/20100101 Firefox/127.0",
      ),
    ).toBe("Firefox on Windows");
  });

  it("names Safari on iOS", () => {
    expect(
      deviceLabel(
        "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 " +
          "(KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1",
      ),
    ).toBe("Safari on iOS");
  });

  it("prefers Edge over Chrome when both tokens are present", () => {
    expect(
      deviceLabel(
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
          "(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 Edg/126.0.0.0",
      ),
    ).toBe("Edge on Windows");
  });

  it("names Chrome on Android", () => {
    expect(
      deviceLabel(
        "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 " +
          "(KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36",
      ),
    ).toBe("Chrome on Android");
  });

  it("returns the OS alone when no browser matches", () => {
    expect(deviceLabel("curl/8.0 (Linux x86_64)")).toBe("Linux");
  });

  it("falls back to Unknown device for blank and unknown agents", () => {
    for (const agent of ["", "   ", "SomeBot/1.0", undefined, null]) {
      expect(deviceLabel(agent)).toBe("Unknown device");
    }
  });
});
