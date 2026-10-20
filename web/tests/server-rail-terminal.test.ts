// JUS-245: the floating terminal window minimizes onto the rail beside the
// code editor, with its own count badge and a click-to-restore control.
/* global document:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { beforeEach, describe, expect, it } from "vitest";

import { useEditorStore } from "../src/features/files/stores/editor";
import type { EditorTab } from "../src/features/files/stores/editor";
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

/** makeTerminal builds one minimal window tab for the badge count. */
function makeTerminal(id: string): TerminalTab {
  return {
    id,
    serverId: "server-1",
    serverName: "prod-01",
    target: "host",
    root: "volumes",
    cwd: "app",
    title: `prod-01 · app ${id}`,
    mode: "window",
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
  };
}

/** makeEditor builds one minimal editor tab for the coexist badge. */
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

/** railRouter is the minimal route table the rail reads. */
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
});

/** mountRail mounts the rail with the given terminal/editor tab counts. */
async function mountRail(terminalCount: number, editorCount = 0) {
  const router = railRouter();
  router.push("/");
  await router.isReady();
  useServersStore().servers = [];
  const terminal = useTerminalStore();
  terminal.tabs = Array.from({ length: terminalCount }, (_, index) =>
    makeTerminal(`t${index}`),
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
  return { wrapper, windowStore };
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
    // One divider against the server list, one against Overview/Alert.
    expect(wrapper.findAll(".rail-foot > .rail-sep")).toHaveLength(2);
    await wrapper.find(".rail-btn--terminal").trigger("click");
    expect(windowStore.minimized).toBe(false);
    await wrapper.find(".rail-btn--terminal").trigger("click");
    expect(windowStore.minimized).toBe(true);
    wrapper.unmount();
  });
});
