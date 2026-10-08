// GitHub App API client and store (GS-5, JUS-61 fix round 1): branch URL
// validation/encoding and the clearErrors action, with a mocked axios layer.
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/shared/api/http", () => ({
  http: { get: vi.fn(), delete: vi.fn(), post: vi.fn() },
}));

import { createPinia, setActivePinia } from "pinia";

import { listGitHubBranches } from "@/features/applications/api/githubApp";
import { useGitHubAppStore } from "@/features/applications/stores/githubApp";
import { http } from "@/shared/api/http";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const get = vi.mocked(http.get);

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.clearAllMocks();
});

describe("listGitHubBranches validates the repository", () => {
  it("encodes owner and name", async () => {
    get.mockResolvedValueOnce({ data: { branches: [] } });
    await listGitHubBranches("app-1", "acme/web");
    expect(get).toHaveBeenCalledWith("/providers/github-app/app-1/repos/acme/web/branches");
  });

  it("rejects malformed repositories without a request", async () => {
    for (const repo of ["", "no-slash", "a/b/c", "/web", "acme/"]) {
      await expect(listGitHubBranches("app-1", repo)).rejects.toThrow();
    }
    expect(get).not.toHaveBeenCalled();
  });
});

describe("github-app store clearErrors", () => {
  it("drops banners and raw errors so a locale change cannot resurrect them", () => {
    setActivePinia(createPinia());
    const store = useGitHubAppStore();
    store.error = "old";
    store.reposError = "old repos";
    store.clearErrors();
    expect(store.error).toBeNull();
    expect(store.reposError).toBeNull();
  });
});
