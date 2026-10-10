// Unit tests (JUS-62 fix round 1): the GitLab provider API client and store
// actions for branches, disconnect, auto-provision and setup-info.
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/shared/api/http", () => ({
  http: { get: vi.fn(), post: vi.fn(), delete: vi.fn() },
}));

import { http } from "@/shared/api/http";
import {
  autoProvisionGitLab,
  deleteProvider,
  gitlabSetupInfo,
  gitlabCallbackUrl,
  listBranches,
  resetControlPlaneUrlCache,
  resolveGitlabCallbackUrl,
} from "@/features/applications/api/providers";
import { useProvidersStore } from "@/features/applications/stores/providers";

const get = vi.mocked(http.get);
const post = vi.mocked(http.post);
const remove = vi.mocked(http.delete);

beforeEach(() => {
  setActivePinia(createPinia());
  vi.clearAllMocks();
  resetControlPlaneUrlCache();
});

describe("provider branches API", () => {
  it("lists branches with the repo query and unwraps the envelope", async () => {
    get.mockResolvedValueOnce({
      data: { branches: [{ name: "main", commit: "abc", protected: true }] },
    });
    const branches = await listBranches("p-1", "acme/demo");
    expect(get).toHaveBeenCalledWith("/providers/p-1/branches", {
      params: { repo: "acme/demo" },
    });
    expect(branches).toEqual([{ name: "main", commit: "abc", protected: true }]);
  });

  it("defaults a missing list to empty", async () => {
    get.mockResolvedValueOnce({ data: {} });
    expect(await listBranches("p-1", "acme/demo")).toEqual([]);
  });
});

describe("provider disconnect API", () => {
  it("deletes the stored connection", async () => {
    remove.mockResolvedValueOnce({ data: { deleted: true } });
    await deleteProvider("p-1");
    expect(remove).toHaveBeenCalledWith("/providers/p-1");
  });
});

describe("gitlab auto-provision API", () => {
  it("posts the one-time details and returns the stored connection", async () => {
    const provider = { id: "p-1", provider: "gitlab", connected: false };
    post.mockResolvedValueOnce({ data: provider });
    const input = {
      base_url: "https://git.example",
      admin_token: "one-time",
      redirect_url: "https://cp.example/api/v1/providers/gitlab/callback",
    };
    expect(await autoProvisionGitLab(input)).toEqual(provider);
    expect(post).toHaveBeenCalledWith("/providers/gitlab/auto-provision", input);
  });
});

describe("gitlab setup-info API", () => {
  it("passes both URLs as query params", async () => {
    const info = {
      base_url: "https://git.example",
      redirect_uri: "https://cp.example/api/v1/providers/gitlab/callback",
      scopes: "api read_user read_repository",
    };
    get.mockResolvedValueOnce({ data: info });
    expect(
      await gitlabSetupInfo("https://git.example", info.redirect_uri),
    ).toEqual(info);
    expect(get).toHaveBeenCalledWith("/providers/gitlab/setup-info", {
      params: { base_url: "https://git.example", redirect_url: info.redirect_uri },
    });
  });
});

describe("providers store gitlab actions", () => {
  it("fetchBranches caches per provider repository", async () => {
    get.mockResolvedValue({ data: { branches: [] } });
    const store = useProvidersStore();
    await store.fetchBranches("p-1", "acme/demo");
    await store.fetchBranches("p-1", "acme/other");
    expect(get).toHaveBeenCalledTimes(2);
    expect(store.branchesOf("p-1", "acme/demo")).toEqual([]);
    expect(store.branchesOf("p-2", "acme/demo")).toEqual([]);
  });

  it("disconnectProvider forgets the connection and its caches", async () => {
    remove.mockResolvedValueOnce({ data: { deleted: true } });
    get
      .mockResolvedValueOnce({
        data: { providers: [{ id: "p-1", provider: "gitlab", connected: true }] },
      })
      .mockResolvedValueOnce({
        data: { repos: [{ id: "1", full_name: "acme/demo" }] },
      })
      .mockResolvedValueOnce({ data: { branches: [{ name: "main" }] } });
    const store = useProvidersStore();
    await store.fetchProviders();
    await store.fetchRepos("p-1");
    await store.fetchBranches("p-1", "acme/demo");
    await store.disconnectProvider("p-1");
    expect(remove).toHaveBeenCalledWith("/providers/p-1");
    expect(store.providers).toEqual([]);
    expect(store.reposOf("p-1")).toEqual([]);
    expect(store.branchesOf("p-1", "acme/demo")).toEqual([]);
  });

  it("provisionGitLab stores the connection and refreshes the list", async () => {
    const created = { id: "p-9", provider: "gitlab", connected: false };
    post.mockResolvedValueOnce({ data: created });
    get.mockResolvedValueOnce({ data: { providers: [created] } });
    const store = useProvidersStore();
    const input = {
      base_url: "https://git.example",
      admin_token: "one-time",
      redirect_url: "https://cp.example/api/v1/providers/gitlab/callback",
    };
    expect(await store.provisionGitLab(input)).toEqual(created);
    expect(store.providers).toEqual([created]);
  });

  it("fetchSetupInfo returns the manual-application details", async () => {
    const info = { base_url: "", redirect_uri: "https://cp.example/x", scopes: "s" };
    get.mockResolvedValueOnce({ data: info });
    const store = useProvidersStore();
    expect(await store.fetchSetupInfo("", "https://cp.example/x")).toEqual(info);
  });
});

describe("resolveGitlabCallbackUrl", () => {
  it("uses the control-plane URL from the API when set", async () => {
    get.mockResolvedValueOnce({
      data: { settings: { general: { control_plane_url: { value: "https://cp.example/" } } } },
    });
    await expect(resolveGitlabCallbackUrl()).resolves.toBe(
      "https://cp.example/api/v1/providers/gitlab/callback",
    );
    expect(get).toHaveBeenCalledWith("/instance/settings");
  });

  it("falls back to the current origin when unset", async () => {
    get.mockResolvedValueOnce({
      data: { settings: { general: { control_plane_url: { value: "" } } } },
    });
    await expect(resolveGitlabCallbackUrl()).resolves.toBe(gitlabCallbackUrl());
  });

  it("falls back to the current origin on API failure", async () => {
    get.mockRejectedValueOnce({ status: 403, message: "forbidden" });
    await expect(resolveGitlabCallbackUrl()).resolves.toBe(gitlabCallbackUrl());
  });
});
