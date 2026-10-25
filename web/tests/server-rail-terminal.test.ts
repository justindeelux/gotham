/* global document:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { beforeEach, describe, expect, it } from "vitest";

import { useEditorStore } from "../src/features/files/stores/editor";
import type { EditorTab } from "../src/features/files/stores/editor";
import { useAuthStore } from "../src/features/auth";
import { useServersStore } from "../src/features/servers/stores/servers";
import { useTerminalStore } from "../src/features/terminal/stores/terminal";
import type { TerminalTab } from "../src/features/terminal/stores/terminal";
import { useTerminalWindowStore } from "../src/features/terminal/stores/terminalWindow";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "../src/shared/i18n";

const { default: ServerRail } = await import(
  "../src/features/servers/components/ServerRail.vue"
);

function makeTerminal(
  id: string,
  mode: "page" | "window" = "window",
  sourcePath = "",
): TerminalTab {
  return {
    id,
    serverId: "server-1",
    serverName: "prod-01",
    target: "host",
    root: "volumes",
    cwd: "app",
    title: `prod-01 · app ${id}`,
    mode,
    sessionId: `session-${id}`,
    status: "connected",
    cols: 120,
    rows: 32,
    shell: "/bin/bash",
    serviceUser: "gotham-agent",
    exitCode: null,
    exitSignal: "",
    errorCode: null,
    createdAt: 0,
    sourcePath,
  };
}

function makeEditor(id: string): EditorTab {
  return {
    id,
    kind: "file",
    root: "volumes",
    path: `e${id}.md`,
    name: `e${id}.md`,
    doc: "",
    original: "",
    etag: '"1"',
    eol: "\n",
    bom: false,
    encoding: "utf-8",
    size: 0,
    mode: 0o100644,
    truncated: false,
    language: "markdown",
    languageManual: false,
    status: "ready",
    error: "",
    saving: false,
  };
}

function railRouter(): ReturnType<typeof createRouter> {
  const stub = { template: "<div />" };
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "dashboard", component: stub },
      { path: "/servers", name: "servers", component: stub },
      { path: "/servers/:id", name: "server-detail", component: stub },
    ],
  });
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  document.body.innerHTML = "";
  setActivePinia(createPinia());
  useAuthStore().user = {
    id: "u-1",
    email: "ada@gotham.dev",
    display_name: "Ada",
    created_at: "2026-01-01T00:00:00Z",
    role: "owner",
  };
});

async function mountRail(
  terminalCount: number,
  editorCount = 0,
  mode: "page" | "window" = "window",
) {
  const router = railRouter();
  router.push("/");
  await router.isReady();
  useServersStore().servers = [];
  const terminal = useTerminalStore();
  terminal.tabs = Array.from({ length: terminalCount }, (_, index) =>
    makeTerminal(`t${index}`, mode, mode === "page" ? `/servers/server-1` : ""),
  );
  const editor = useEditorStore();
  editor.tabs = Array.from({ length: editorCount }, (_, index) =>
    makeEditor(String(index)),
  );
  const windowStore = useTerminalWindowStore();
  windowStore.setMinimized(true);
  const wrapper = mount(ServerRail, {
    attachTo: document.body,
    global: { plugins: [router, i18n] },
  });
  await flushPromises();
  return { wrapper, windowStore, router };
}

describe("ServerRail terminal icon (JUS-245)", () => {
  it("shows the terminal count with an accessible name", async () => {
    const { wrapper } = await mountRail(2);
    const button = wrapper.find(".rail-btn--terminal");
    expect(button.exists()).toBe(true);
    expect(button.attributes("aria-label")).toBe("Terminal window, 2 open");
    expect(button.find(".rail-badge").text()).toBe("2");
    wrapper.unmount();
  });

  it("caps a large count at 9+ and hides with no window tabs", async () => {
    const { wrapper } = await mountRail(12);
    expect(wrapper.find(".rail-btn--terminal .rail-badge").text()).toBe("9+");
    useTerminalStore().tabs = [];
    await flushPromises();
    expect(wrapper.find(".rail-btn--terminal").exists()).toBe(false);
    wrapper.unmount();
  });

  it("restores the window on click and coexists with the editor badge", async () => {
    const { wrapper, windowStore } = await mountRail(1, 2);
    expect(windowStore.minimized).toBe(true);
    expect(wrapper.find(".rail-btn--editor").exists()).toBe(true);
    expect(wrapper.find(".rail-btn--terminal").exists()).toBe(true);
    expect(wrapper.findAll(".rail-foot > .rail-sep")).toHaveLength(2);
    await wrapper.find(".rail-btn--terminal").trigger("click");
    expect(windowStore.minimized).toBe(false);
    await wrapper.find(".rail-btn--terminal").trigger("click");
    expect(windowStore.minimized).toBe(true);
    wrapper.unmount();
  });

  it("counts page-only sessions and resumes their source page (JUS-246)", async () => {
    const { wrapper, router } = await mountRail(1, 0, "page");
    const button = wrapper.find(".rail-btn--terminal");
    expect(button.exists()).toBe(true);
    expect(button.attributes("aria-label")).toBe("Page terminals, 1 open");
    expect(button.find(".rail-badge").text()).toBe("1");

    await button.trigger("click");
    await flushPromises();
    expect(router.currentRoute.value.path).toBe("/servers/server-1");
    wrapper.unmount();
  });

  it("counts page and window sessions together", async () => {
    const { wrapper } = await mountRail(2);
    useTerminalStore().tabs = [
      ...useTerminalStore().tabs,
      makeTerminal("page-1", "page", "/terminal?server=server-1"),
    ];
    await flushPromises();
    expect(wrapper.find(".rail-btn--terminal .rail-badge").text()).toBe("3");
    wrapper.unmount();
  });

  it("hides the terminal entry point after a demotion (JUS-246)", async () => {
    const { wrapper } = await mountRail(1);
    expect(wrapper.find(".rail-btn--terminal").exists()).toBe(true);
    useAuthStore().user = {
      ...useAuthStore().user!,
      role: "member",
    };
    await flushPromises();
    expect(wrapper.find(".rail-btn--terminal").exists()).toBe(false);
    wrapper.unmount();
  });
});
