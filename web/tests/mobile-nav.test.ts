// Mobile drawer state: per-mount lifecycle, focus trap, route/viewport close.
import { mount } from "@vue/test-utils";
import { createMemoryHistory, createRouter } from "vue-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h } from "vue";

import type { MobileNav } from "../src/app/layouts/useMobileNav";
import { provideMobileNav, useMobileNav } from "../src/app/layouts/useMobileNav";

type MediaListener = (_event: { matches: boolean }) => void;

let mediaListeners: MediaListener[] = [];

function stubMatchMedia(): void {
  mediaListeners = [];
  globalThis.window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: true,
    media: query,
    addEventListener: vi.fn((_type: string, listener: MediaListener) => {
      mediaListeners.push(listener);
    }),
    removeEventListener: vi.fn((_type: string, listener: MediaListener) => {
      mediaListeners = mediaListeners.filter((item) => item !== listener);
    }),
  })) as unknown as typeof globalThis.window.matchMedia;
}

let api: MobileNav | null = null;

const Harness = defineComponent({
  setup() {
    api = provideMobileNav();
    return () => h("div");
  },
});

function testRouter(): ReturnType<typeof createRouter> {
  const stub = { template: "<div />" };
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/a", name: "a", component: stub },
      { path: "/b", name: "b", component: stub },
    ],
  });
}

async function mountNav(path = "/a"): Promise<ReturnType<typeof mount>> {
  api = null;
  const router = testRouter();
  await router.push(path);
  await router.isReady();
  const wrapper = mount(Harness, {
    attachTo: globalThis.document.body,
    global: { plugins: [router] },
  });
  await router.isReady();
  return wrapper;
}

function fireViewport(matches: boolean): void {
  for (const listener of [...mediaListeners]) {
    listener({ matches });
  }
}

beforeEach(() => {
  stubMatchMedia();
  globalThis.document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("per-mount state", () => {
  it("starts closed on every mount", async () => {
    const first = await mountNav();
    expect(api?.mobileNavOpen.value).toBe(false);
    api?.toggleNav();
    expect(api?.mobileNavOpen.value).toBe(true);
    first.unmount();
    await mountNav();
    expect(api?.mobileNavOpen.value).toBe(false);
  });

  it("registers one viewport listener per mount and removes it on unmount", async () => {
    const first = await mountNav();
    expect(mediaListeners).toHaveLength(1);
    first.unmount();
    expect(mediaListeners).toHaveLength(0);
    await mountNav();
    expect(mediaListeners).toHaveLength(1);
  });

  it("adds the keydown listener only while open", async () => {
    const add = vi.spyOn(globalThis.window, "addEventListener");
    const remove = vi.spyOn(globalThis.window, "removeEventListener");
    await mountNav();
    expect(add).not.toHaveBeenCalledWith("keydown", expect.any(Function));
    api?.toggleNav();
    await Promise.resolve();
    expect(add).toHaveBeenCalledWith("keydown", expect.any(Function));
    api?.closeMobileNav();
    await Promise.resolve();
    expect(remove).toHaveBeenCalledWith("keydown", expect.any(Function));
  });
});

describe("drawer behaviour", () => {
  it("closes on Escape and restores focus", async () => {
    await mountNav();
    const opener = globalThis.document.createElement("button");
    globalThis.document.body.appendChild(opener);
    opener.focus();
    api?.toggleNav();
    expect(api?.mobileNavOpen.value).toBe(true);
    await Promise.resolve();
    await Promise.resolve();
    globalThis.window.dispatchEvent(new globalThis.KeyboardEvent("keydown", { key: "Escape" }));
    expect(api?.mobileNavOpen.value).toBe(false);
    expect(globalThis.document.activeElement).toBe(opener);
  });

  it("traps Tab inside the drawer", async () => {
    await mountNav();
    const aside = globalThis.document.createElement("aside");
    const first = globalThis.document.createElement("button");
    const last = globalThis.document.createElement("button");
    aside.appendChild(first);
    aside.appendChild(last);
    globalThis.document.body.appendChild(aside);
    for (const element of [aside, first, last]) {
      Object.defineProperty(element, "offsetParent", { value: globalThis.document.body });
    }
    if (api) {
      api.sidebarRef.value = aside;
    }
    api?.toggleNav();
    await Promise.resolve();
    await Promise.resolve();
    last.focus();
    globalThis.window.dispatchEvent(new globalThis.KeyboardEvent("keydown", { key: "Tab" }));
    expect(globalThis.document.activeElement).toBe(first);
    globalThis.window.dispatchEvent(
      new globalThis.KeyboardEvent("keydown", { key: "Tab", shiftKey: true }),
    );
    expect(globalThis.document.activeElement).toBe(last);
  });

  it("closes on route change", async () => {
    const router = testRouter();
    await router.push("/a");
    await router.isReady();
    api = null;
    mount(Harness, {
      attachTo: globalThis.document.body,
      global: { plugins: [router] },
    });
    api?.toggleNav();
    expect(api?.mobileNavOpen.value).toBe(true);
    await router.push("/b");
    expect(api?.mobileNavOpen.value).toBe(false);
  });

  it("closes when the viewport grows past 1024px", async () => {
    await mountNav();
    api?.toggleNav();
    expect(api?.mobileNavOpen.value).toBe(true);
    fireViewport(true);
    expect(api?.mobileNavOpen.value).toBe(true);
    fireViewport(false);
    expect(api?.mobileNavOpen.value).toBe(false);
  });
});

describe("injection contract", () => {
  it("throws a clear error without the layout provider", () => {
    const Orphan = defineComponent({
      setup() {
        useMobileNav();
        return () => h("div");
      },
    });
    expect(() => mount(Orphan)).toThrowError(/inside AppLayout/);
  });
});
