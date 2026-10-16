// Dashboard recent-deployments store (JUS-193). The aggregate feed loads once,
// is bounded by the feed limit, and a read failure is retained and localized
// (the panel never fabricates rows).
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/applications", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/applications")>();
  return { ...actual, listRecentDeployments: vi.fn() };
});

import { listRecentDeployments } from "@/features/applications";
import type { RecentDeployment } from "@/features/applications";
import {
  recentDeploymentsLimit,
  useRecentDeploymentsStore,
} from "@/features/dashboard/stores/deployments";

function row(id: string): RecentDeployment {
  return {
    id,
    application_id: "app-1",
    application_name: "storefront",
    application_repo: "acme/storefront",
    application_branch: "main",
    kind: "deploy",
    state: "running",
    image_tag: "gotham/storefront:1",
    registry_image: "",
    digest: "",
    error: "",
    attempt: 1,
    container_id: "",
    rollback_from: "",
    started_at: "2026-10-17T00:00:00Z",
    finished_at: "2026-10-17T00:00:05Z",
    created_at: "2026-10-17T00:00:00Z",
    updated_at: "2026-10-17T00:00:05Z",
  };
}

beforeEach(() => {
  setActivePinia(createPinia());
  vi.mocked(listRecentDeployments).mockReset();
});

describe("dashboard recent-deployments store", () => {
  it("loads the bounded feed once and exposes the rows", async () => {
    vi.mocked(listRecentDeployments).mockResolvedValue([row("d1")]);
    const store = useRecentDeploymentsStore();

    await store.fetchRecentDeployments();

    expect(vi.mocked(listRecentDeployments)).toHaveBeenCalledTimes(1);
    expect(vi.mocked(listRecentDeployments)).toHaveBeenCalledWith(
      recentDeploymentsLimit,
    );
    expect(store.deployments.map((deployment) => deployment.id)).toEqual([
      "d1",
    ]);
    expect(store.loading).toBe(false);
    expect(store.error).toBeNull();
  });

  it("retains a read failure, clears the rows and resolves the message", async () => {
    vi.mocked(listRecentDeployments).mockRejectedValue({
      message: "deploy: list recent deployments: boom",
      status: 500,
    });
    const store = useRecentDeploymentsStore();

    await store.fetchRecentDeployments();

    expect(store.deployments).toEqual([]);
    expect(store.loading).toBe(false);
    expect(store.error).toBe("Request failed list recent deployments: boom");
  });
});
