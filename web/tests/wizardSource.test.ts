// Unit tests for the wizard source-selection helpers extracted from
// CreateAppWizard during the JUS-24 split (F2-applications).

import { describe, expect, it } from "vitest";

import type { ProviderRepo } from "../src/features/applications/api/providers";
import { cloneUrlFor, suggestAppName } from "../src/features/applications/utils/wizardSource";

function repo(overrides: Partial<ProviderRepo>): ProviderRepo {
  return {
    full_name: "owner/repo",
    name: "repo",
    private: false,
    default_branch: "main",
    clone_url: "https://github.com/owner/repo.git",
    ssh_url: "git@github.com:owner/repo.git",
    ...overrides,
  } as ProviderRepo;
}

describe("cloneUrlFor", () => {
  it("keeps the https URL for public repositories", () => {
    expect(cloneUrlFor(repo({ private: false }))).toBe("https://github.com/owner/repo.git");
  });

  it("stores the provider ssh_url for private repositories", () => {
    expect(cloneUrlFor(repo({ private: true }))).toBe("git@github.com:owner/repo.git");
  });

  it("returns empty when a private repository reports no SSH URL", () => {
    expect(cloneUrlFor(repo({ private: true, ssh_url: "  " }))).toBe("");
  });
});

describe("suggestAppName", () => {
  it("lowercases and slugifies the repository name", () => {
    expect(suggestAppName("My_Cool.App!")).toBe("my-cool-app-");
  });

  it("caps the suggestion at 31 characters", () => {
    expect(suggestAppName("a".repeat(40))).toHaveLength(31);
  });
});
