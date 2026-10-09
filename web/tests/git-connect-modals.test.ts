// Connect GitHub / GitLab modal layout (JUS-74): both dialogs are ~520px
// wide with stacked labels, the hint directly under its input and the
// actions in a right-aligned footer (primary last). jsdom never applies
// stylesheets, so the width is asserted from the NModal style prop text
// (same pattern as the JUS-16/17/18 suite) plus structural mounts.
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { NForm, NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { defineComponent, h, ref } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/applications/composables/useGitSourcesPage", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/applications/composables/useGitSourcesPage")>();
  return { ...actual, useGitSourcesPage: vi.fn() };
});

import GitLabConnectDialog from "@/features/applications/components/GitLabConnectDialog.vue";
import GitSourcesPage from "@/features/applications/pages/GitSourcesPage.vue";
import { useGitSourcesPage } from "@/features/applications/composables/useGitSourcesPage";
import applicationsEn from "@/features/applications/locales/en";
import applicationsVi from "@/features/applications/locales/vi";
import { useTeamsStore } from "@/features/teams";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

const mockedPage = vi.mocked(useGitSourcesPage);

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");

/** readSfc returns the raw source of one single-file component. */
function readSfc(relativePath: string): string {
  return readFileSync(resolve(webRoot, relativePath), "utf8");
}

const gitHubSource = readSfc("src/features/applications/pages/GitSourcesPage.vue");
const gitLabSource = readSfc("src/features/applications/components/GitLabConnectDialog.vue");

/** modalWidthPx reads the NModal style width in pixels. */
function modalWidthPx(source: string): number {
  const match = source.match(/width:\s*(\d+)px/);
  expect(match, "expected an NModal style width in px").not.toBeNull();
  return Number(match?.[1] ?? Number.NaN);
}

function shell(child: object) {
  return defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(child as never) });
    },
  });
}

function seedTeams(): void {
  const teams = useTeamsStore();
  teams.teams = [{ id: "team-1", name: "Acme", is_personal: false, role: "owner" }] as never;
  teams.activeTeamId = "team-1";
}

beforeEach(() => {
  setActivePinia(createPinia());
  resetLocaleState();
  i18n.global.mergeLocaleMessage("en", { applications: applicationsEn });
  i18n.global.mergeLocaleMessage("vi", { applications: applicationsVi });
  syncComposerLocale("en");
  vi.clearAllMocks();
  seedTeams();
  mockedPage.mockReturnValue({
    rows: ref([]),
    loading: ref(false),
    error: ref(null),
    usageKnown: ref(true),
    usageLoading: ref(false),
    disconnectError: ref(null),
    disconnectNames: ref([]),
    refresh: vi.fn().mockResolvedValue(undefined),
    connectGitHub: vi.fn().mockResolvedValue(undefined),
    reconnect: vi.fn().mockResolvedValue(undefined),
    disconnect: vi.fn().mockResolvedValue({ applicationsUsing: 0 }),
    gitLabManualInfo: vi.fn(),
    provisionGitLabAuto: vi.fn(),
    provisionGitLabManual: vi.fn(),
    startAuthorize: vi.fn(),
  } as never);
});

describe("JUS-74 connect modal width", () => {
  it.each([
    ["GitHub", gitHubSource],
    ["GitLab", gitLabSource],
  ])("%s dialog is ~520px wide (<= 600px)", (_name, source) => {
    const width = modalWidthPx(source as string);
    expect(width).toBeLessThanOrEqual(600);
    expect(width).toBeGreaterThanOrEqual(480);
  });

  it("stacks labels above inputs with a right-aligned footer", () => {
    for (const source of [gitHubSource, gitLabSource]) {
      expect(source).toContain('label-placement="top"');
      expect(source).toContain("#footer");
      expect(source).toContain('justify="end"');
    }
  });

  it("keeps header/footer fixed with only the body scrolling", () => {
    for (const source of [gitHubSource, gitLabSource]) {
      expect(source).toContain("calc(100vh - 64px)");
      expect(source).toContain("overflow-y: auto");
    }
  });
});

describe("JUS-74 GitHub modal structure", () => {
  it("renders the name field stacked with its hint and primary last", async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/", name: "home", component: { template: "<div />" } }],
    });
    await router.push("/");
    await router.isReady();
    const wrapper = mount(shell(GitSourcesPage), {
      global: { plugins: [router, i18n], stubs: { teleport: true } },
    });
    await flushPromises();
    await wrapper.find(".page-actions button").trigger("click");
    await flushPromises();
    expect(wrapper.find("input#github-app-name-input").exists()).toBe(true);
    const form = wrapper.findComponent(NForm);
    expect(form.exists()).toBe(true);
    expect(form.props("labelPlacement")).toBe("top");
    expect(wrapper.text()).toContain("Letters, digits and dashes");
    const footerButtons = wrapper.findAll(".dialog-card").find((c) => c.text().includes("Connect GitHub"))
      ?.findAll("button").map((b) => b.text()) ?? [];
    expect(footerButtons[footerButtons.length - 1]).toBe("Connect GitHub");
  });
});

describe("JUS-74 GitLab dialog structure", () => {
  it("renders the instance field stacked with its hint and primary last", async () => {
    const wrapper = mount(GitLabConnectDialog, {
      props: { show: true },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();
    expect(wrapper.find("input#gitlab-instance-input").exists()).toBe(true);
    const form = wrapper.findComponent(NForm);
    expect(form.exists()).toBe(true);
    expect(form.props("labelPlacement")).toBe("top");
    expect(wrapper.text()).toContain("gitlab.com or a self-hosted instance");
    const buttons = wrapper.findAll(".dialog-card button").map((b) => b.text());
    expect(buttons[buttons.length - 1]).toMatch(/Provision|Save/);
  });
});
