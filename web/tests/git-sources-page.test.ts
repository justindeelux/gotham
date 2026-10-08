// Git sources management page (GS-10, JUS-66): row mapping, usage guards,
// connect clients, the disconnect 409 flow and the write-only secrets.
import { createPinia, setActivePinia } from "pinia";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/shared/api/http", () => ({
  http: { get: vi.fn(), post: vi.fn(), delete: vi.fn() },
}));

import GitLabConnectDialog from "@/features/applications/components/GitLabConnectDialog.vue";
import {
  authorizeProvider,
  createProvider,
  gitlabCallbackUrl,
} from "@/features/applications/api/providers";
import {
  appsForGitHubApp,
  appsForProvider,
  hostOf,
  isLegacyProvider,
  namesFromConflict,
  rowForGitHubApp,
  rowForProvider,
  useGitSourcesPage,
} from "@/features/applications/composables/useGitSourcesPage";
import { http } from "@/shared/api/http";
import applicationsEn from "@/features/applications/locales/en";
import applicationsVi from "@/features/applications/locales/vi";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";
import type { Application } from "@/features/applications/api/applications";
import type { SourceProvider } from "@/features/applications/api/providers";

const get = vi.mocked(http.get);
const post = vi.mocked(http.post);
const del = vi.mocked(http.delete);

const app = (over: Partial<Application>): Application =>
  ({
    id: "app-1",
    name: "shop",
    environment_id: "env-1",
    environment_name: "prod",
    project_id: "proj-1",
    project_name: "acme",
    provider: "gitlab",
    repo: "acme/shop",
    clone_url: "https://git.example.com/acme/shop.git",
    source_type: "gitlab_app",
    github_app_id: "",
    branch: "main",
    build_pack: "",
    base_domain: "",
    base_domain_disabled: false,
    port: 3000,
    host_port: 0,
    server_id: null,
    server_name: "",
    created_at: "2026-09-01T00:00:00Z",
    updated_at: "2026-09-01T00:00:00Z",
    ...over,
  }) as Application;

const gitlabProvider = (over: Partial<SourceProvider> = {}): SourceProvider => ({
  id: "prov-1",
  provider: "gitlab",
  base_url: "https://git.example.com",
  connected: true,
  scopes: "read_api",
  created_at: "2026-09-28T00:00:00Z",
  updated_at: "2026-09-28T00:00:00Z",
  ...over,
});

beforeEach(() => {
  setActivePinia(createPinia());
  resetLocaleState();
  i18n.global.mergeLocaleMessage("en", { applications: applicationsEn });
  i18n.global.mergeLocaleMessage("vi", { applications: applicationsVi });
  syncComposerLocale("en");
  vi.clearAllMocks();
});

describe("hostOf", () => {
  it("lowercases the hostname and blanks unparsable URLs", () => {
    expect(hostOf("https://GIT.example.com/acme/shop.git")).toBe("git.example.com");
    expect(hostOf("not a url")).toBe("");
  });
});

describe("usage guards mirror the server disconnect checks", () => {
  it("links GitHub App applications by connection id", () => {
    const apps = [
      app({ id: "a", name: "shop", provider: "github", github_app_id: "gh-1" }),
      app({ id: "b", name: "blog", provider: "github", github_app_id: "gh-2" }),
    ];
    expect(appsForGitHubApp(apps, "gh-1")).toEqual(["shop"]);
  });

  it("matches providers by slug and clone host, skipping GitHub App links", () => {
    const apps = [
      app({ name: "shop" }),
      app({ name: "other", clone_url: "https://git.other.com/acme/other.git" }),
      app({ name: "hooked", provider: "github", clone_url: "https://github.com/acme/x.git", github_app_id: "gh-1" }),
    ];
    expect(appsForProvider(apps, gitlabProvider())).toEqual(["shop"]);
  });

  it("marks every non-GitLab OAuth connection legacy read-only", () => {
    expect(isLegacyProvider(gitlabProvider())).toBe(false);
    expect(isLegacyProvider(gitlabProvider({ provider: "github" }))).toBe(true);
    expect(isLegacyProvider(gitlabProvider({ provider: "gitea" }))).toBe(true);
  });
});

describe("row mapping", () => {
  it("maps a pending GitHub App to needs-attention without accounts", () => {
    const row = rowForGitHubApp(
      {
        id: "gh-1",
        app_id: 11,
        slug: "gotham-ci",
        name: "",
        base_url: "https://github.com",
        connected: false,
        created_at: "2026-10-02T00:00:00Z",
        installations: [],
      },
      [],
      null,
    );
    expect(row.connected).toBe(false);
    expect(row.account).toBe("—");
    expect(row.installations).toBe(0);
    expect(row.legacy).toBe(false);
  });

  it("maps a GitLab provider with usage names and scopes", () => {
    const row = rowForProvider(gitlabProvider(), [app({ name: "shop" })], 12);
    expect(row.title).toBe("GitLab");
    expect(row.apps).toEqual(["shop"]);
    expect(row.repos).toBe(12);
    expect(row.legacy).toBe(false);
  });
});

describe("namesFromConflict", () => {
  it("splits the 409 application list", () => {
    expect(namesFromConflict("applications still use this connection: shop, blog")).toEqual([
      "shop",
      "blog",
    ]);
    expect(namesFromConflict("no colon at all")).toEqual(["no colon at all"]);
  });
});

describe("provider connect clients", () => {
  it("stores a manual OAuth app", async () => {
    post.mockResolvedValueOnce({ data: gitlabProvider({ connected: false }) });
    const created = await createProvider({
      provider: "gitlab",
      base_url: "https://git.example.com",
      client_id: "cid",
      client_secret: "write-only",
      redirect_url: "https://cp.example/api/v1/providers/gitlab/callback",
    });
    expect(post).toHaveBeenCalledWith("/providers", {
      provider: "gitlab",
      base_url: "https://git.example.com",
      client_id: "cid",
      client_secret: "write-only",
      redirect_url: "https://cp.example/api/v1/providers/gitlab/callback",
    });
    expect(created.connected).toBe(false);
  });

  it("starts the PKCE authorize", async () => {
    get.mockResolvedValueOnce({ data: { url: "https://git.example.com/oauth/authorize?x=1", state: "s" } });
    await expect(authorizeProvider("prov-1")).resolves.toEqual({
      url: "https://git.example.com/oauth/authorize?x=1",
      state: "s",
    });
    expect(get).toHaveBeenCalledWith("/providers/prov-1/authorize");
  });

  it("points the OAuth redirect at this control plane", () => {
    expect(gitlabCallbackUrl().endsWith("/api/v1/providers/gitlab/callback")).toBe(true);
  });
});

describe("disconnect 409 flow", () => {
  it("keeps the 409 application names on the error", async () => {
    del.mockRejectedValueOnce({
      status: 409,
      message: "applications still use this connection: shop, blog",
    });
    const page = useGitSourcesPage();
    await expect(
      page.disconnect({
        kind: "provider",
        id: "prov-1",
        title: "GitLab",
        subtitle: "OAuth · PKCE",
        connected: true,
        account: "git.example.com",
        instance: "read_api",
        installations: null,
        repos: null,
        apps: ["shop", "blog"],
        createdAt: "2026-09-28T00:00:00Z",
        legacy: false,
      }),
    ).rejects.toBeTruthy();
    expect(page.disconnectNames.value).toEqual(["shop", "blog"]);
    expect(page.disconnectError.value ?? "").toContain("shop");
  });

  it("reports the GitHub App usage count on success", async () => {
    del.mockResolvedValueOnce({ data: { deleted: true, applications_using: 2 } });
    get.mockResolvedValue({ data: { apps: [] } });
    post.mockResolvedValue({ data: {} });
    const page = useGitSourcesPage();
    const result = await page.disconnect({
      kind: "github-app",
      id: "gh-1",
      title: "GitHub App",
      subtitle: "gotham-ci",
      connected: true,
      account: "acme",
      instance: "github.com",
      installations: 1,
      repos: 3,
      apps: ["shop", "blog"],
      createdAt: "2026-10-02T00:00:00Z",
      legacy: false,
    });
    expect(result.applicationsUsing).toBe(2);
  });

  it("marks usage unknown when the application list fails", async () => {
    get.mockImplementation((url: string) => {
      if (url === "/applications") {
        return Promise.reject({ status: 500, message: "boom" });
      }
      return Promise.resolve({ data: { apps: [], providers: [] } });
    });
    const page = useGitSourcesPage();
    await page.refresh();
    expect(page.usageKnown.value).toBe(false);
    expect(page.usageLoading.value).toBe(false);
  });
});

describe("GitLabConnectDialog secrets", () => {
  it("clears the one-time admin token on submit", async () => {
    post.mockResolvedValueOnce({ data: gitlabProvider({ connected: false }) });
    get.mockResolvedValueOnce({ data: { providers: [] } });
    get.mockResolvedValueOnce({ data: { url: "https://git.example.com/oauth/x", state: "s" } });
    const wrapper = mount(GitLabConnectDialog, {
      props: { show: true },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();
    const token = wrapper.find("input#gitlab-admin-token-input");
    await token.setValue("super-secret-admin-token");
    await wrapper.findAll("button").find((b) => b.text().includes("Provision"))?.trigger("click");
    await flushPromises();
    expect(post).toHaveBeenCalledWith(
      "/providers/gitlab/auto-provision",
      expect.objectContaining({ admin_token: "super-secret-admin-token" }),
    );
    // The write-only token never survives the submit.
    expect((token.element as { value: string }).value).toBe("");
  });

  it("clears secrets on a failed submit and names the Reconnect action", async () => {
    post.mockResolvedValueOnce({ data: gitlabProvider({ connected: false }) });
    get.mockResolvedValueOnce({ data: { providers: [] } });
    get.mockRejectedValueOnce({ status: 500, message: "authorize blew up" });
    const wrapper = mount(GitLabConnectDialog, {
      props: { show: true },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();
    await wrapper.find("input#gitlab-admin-token-input").setValue("one-time-token");
    await wrapper.findAll("button").find((b) => b.text().includes("Provision"))?.trigger("click");
    await flushPromises();
    expect((wrapper.find("input#gitlab-admin-token-input").element as { value: string }).value).toBe("");
    expect(wrapper.text()).toContain("Reconnect");
  });

  it("rejects a whitespace-only admin token", async () => {
    const wrapper = mount(GitLabConnectDialog, {
      props: { show: true },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();
    await wrapper.find("input#gitlab-admin-token-input").setValue("   ");
    const provision = wrapper.findAll("button").find((b) => b.text().includes("Provision"));
    expect(provision?.attributes("disabled")).not.toBeUndefined();
    expect(post).not.toHaveBeenCalled();
  });

  it("associates every label with its input", async () => {
    get.mockResolvedValueOnce({
      data: {
        base_url: "https://git.example.com",
        redirect_uri: "https://cp.example/api/v1/providers/gitlab/callback",
        scopes: "api read_api",
      },
    });
    const wrapper = mount(GitLabConnectDialog, {
      props: { show: true },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();
    const autoLabels = wrapper.findAll("label").map((label) => label.attributes("for") ?? "");
    expect(autoLabels).toContain("gitlab-instance-input");
    expect(autoLabels).toContain("gitlab-admin-token-input");
    for (const id of autoLabels) {
      expect(wrapper.find(`input#${id}`).exists()).toBe(true);
    }
    await wrapper.findAll("button").find((b) => b.text().includes("manual"))?.trigger("click");
    await flushPromises();
    for (const id of ["gitlab-redirect-input", "gitlab-client-id-input", "gitlab-client-secret-input", "gitlab-scopes-input"]) {
      expect(wrapper.find(`label[for="${id}"]`).exists()).toBe(true);
      expect(wrapper.find(`input#${id}`).exists()).toBe(true);
    }
  });

  it("blocks manual submit without a redirect URI", async () => {
    get.mockRejectedValueOnce({ status: 500, message: "setup-info blew up" });
    const wrapper = mount(GitLabConnectDialog, {
      props: { show: true },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();
    await wrapper.findAll("button").find((b) => b.text().includes("manual"))?.trigger("click");
    await flushPromises();
    await wrapper.find("input#gitlab-client-id-input").setValue("cid");
    await wrapper.find("input#gitlab-client-secret-input").setValue("secret");
    const save = wrapper.findAll("button").find((b) => b.text().includes("Save"));
    expect(save?.attributes("disabled")).not.toBeUndefined();
    expect(post).not.toHaveBeenCalled();
  });
});
