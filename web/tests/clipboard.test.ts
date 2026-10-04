import { beforeEach, describe, expect, it, vi } from "vitest";

import { useCopyText } from "@/shared/composables/useCopyText";

/* global window: readonly, document: readonly */

const { mockSuccess, mockError } = vi.hoisted(() => ({
  mockSuccess: vi.fn(),
  mockError: vi.fn(),
}));

vi.mock("naive-ui", async (importOriginal) => {
  const mod = await importOriginal<Record<string, unknown>>();
  return {
    ...mod,
    useMessage: () => ({ success: mockSuccess, error: mockError }),
  };
});

interface StubbedNavigator {
  clipboard?: { write: (_data: unknown[]) => Promise<void> };
  permissions?: { query: () => Promise<{ state: string }> };
}

function navigatorStub(): StubbedNavigator | undefined {
  return (window.navigator as unknown as StubbedNavigator).clipboard !==
    undefined || (window.navigator as unknown as StubbedNavigator).permissions !== undefined
    ? (window.navigator as unknown as StubbedNavigator)
    : undefined;
}

function stubClipboardApi(write: (_data: unknown[]) => Promise<void>): void {
  Object.defineProperty(window.navigator, "clipboard", {
    value: { write: vi.fn(write) },
    configurable: true,
  });
}

function stubPermissionsGranted(): void {
  Object.defineProperty(window.navigator, "permissions", {
    value: {
      query: () =>
        Promise.resolve({
          state: "granted",
          addEventListener: () => undefined,
          removeEventListener: () => undefined,
        }),
    },
    configurable: true,
  });
  vi.stubGlobal("ClipboardItem", class {});
}

async function flushPermissions(): Promise<void> {
  for (let i = 0; i < 10; i += 1) {
    await Promise.resolve();
  }
}

describe("useCopyText", () => {
  const hadClipboard = "clipboard" in window.navigator;
  const hadPermissions = "permissions" in window.navigator;
  const hadExecCommand = "execCommand" in document;

  beforeEach(() => {
    mockSuccess.mockClear();
    mockError.mockClear();
    if (!hadClipboard) {
      delete (window.navigator as unknown as StubbedNavigator).clipboard;
    }
    if (!hadPermissions) {
      delete (window.navigator as unknown as StubbedNavigator).permissions;
    }
    if (!hadExecCommand) {
      delete (document as unknown as { execCommand?: unknown }).execCommand;
    }
    vi.unstubAllGlobals();
  });

  it("copies through the async Clipboard API in a secure context", async () => {
    const write = vi.fn(() => Promise.resolve());
    stubClipboardApi(write);
    stubPermissionsGranted();
    const { copyText } = useCopyText();
    await flushPermissions();
    await copyText("s3cret", "Password");
    expect(write).toHaveBeenCalledOnce();
    expect(mockSuccess).toHaveBeenCalledWith("Password copied to clipboard");
    expect(mockError).not.toHaveBeenCalled();
    expect(navigatorStub()).toBeDefined();
  });

  it("copies through the execCommand fallback with no clipboard API (http)", async () => {
    const execCommand = vi.fn(() => true);
    (document as unknown as { execCommand: unknown }).execCommand = execCommand;
    const { copyText } = useCopyText();
    await copyText("s3cret", "Password");
    expect(execCommand).toHaveBeenCalledWith("copy");
    expect(mockSuccess).toHaveBeenCalledWith("Password copied to clipboard");
    expect(mockError).not.toHaveBeenCalled();
  });

  it("shows the error toast when the execCommand fallback fails", async () => {
    (document as unknown as { execCommand: unknown }).execCommand = vi.fn(() => {
      throw new Error("denied");
    });
    const { copyText } = useCopyText();
    await copyText("s3cret", "Password");
    expect(mockSuccess).not.toHaveBeenCalled();
    expect(mockError).toHaveBeenCalledWith("Could not copy password");
  });

  it("falls back to execCommand when the async write rejects", async () => {
    stubClipboardApi(() => Promise.reject(new Error("denied")));
    stubPermissionsGranted();
    const execCommand = vi.fn(() => true);
    (document as unknown as { execCommand: unknown }).execCommand = execCommand;
    const { copyText } = useCopyText();
    await flushPermissions();
    await copyText("s3cret", "Connection string");
    expect(execCommand).toHaveBeenCalledWith("copy");
    expect(mockSuccess).toHaveBeenCalledWith(
      "Connection string copied to clipboard",
    );
    expect(mockError).not.toHaveBeenCalled();
  });
});
