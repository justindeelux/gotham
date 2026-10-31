/* global window:readonly */
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Incident, IncidentPage } from "@/features/incidents/api/incidents";
import { groupIncidents } from "@/features/incidents/utils/incidents";

const api = vi.hoisted(() => ({
  listIncidents: vi.fn(),
  acknowledgeIncident: vi.fn(),
  snoozeIncident: vi.fn(),
}));

vi.mock("@/features/incidents/api/incidents", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/incidents/api/incidents")>();
  return {
    ...actual,
    listIncidents: api.listIncidents,
    acknowledgeIncident: api.acknowledgeIncident,
    snoozeIncident: api.snoozeIncident,
  };
});

import { useAuthStore } from "@/features/auth";
import { useIncidentsStore } from "@/features/incidents";
import { setSession } from "@/shared/api/token";

function incident(id: string, overrides: Partial<Incident> = {}): Incident {
  return {
    id,
    detector: "runtime_health",
    scope: "project",
    project_id: "project-1",
    resource_type: "application",
    resource_id: `resource-${id}`,
    resource_name: id,
    severity: "warning",
    lifecycle: "firing",
    coverage: "fresh",
    first_observed_at: "2026-01-01T00:00:00Z",
    last_observed_at: "2026-01-01T00:01:00Z",
    last_valid_at: "2026-01-01T00:01:00Z",
    delivery_state: "dispatched",
    delivery_attempts: 1,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:01:00Z",
    ...overrides,
  };
}

function page(
  incidents: Incident[],
  nextCursor = "",
  activeCount = incidents.length,
): IncidentPage {
  return { incidents, next_cursor: nextCursor, active_count: activeCount };
}

function deferred<T>(): {
  promise: Promise<T>;
  resolve: (_value: T) => void;
} {
  let resolve: (_value: T) => void = () => undefined;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

beforeEach(() => {
  window.localStorage.clear();
  setActivePinia(createPinia());
  api.listIncidents.mockReset();
  api.acknowledgeIncident.mockReset();
  api.snoozeIncident.mockReset();
});

describe("incidents store", () => {
  it("clears user-scoped rows, filters and resolved state on logout", () => {
    setSession({
      user: {
        id: "u-1",
        email: "ada@gotham.dev",
        created_at: "2026-01-01T00:00:00Z",
      },
      accessToken: "a",
      refreshToken: "r",
    });
    const auth = useAuthStore();
    const store = useIncidentsStore();
    store.items = [incident("a")];
    store.resolved = [incident("r", { lifecycle: "resolved" })];
    store.activeCount = 1;
    store.severity = "critical";
    store.resourceType = "node";
    store.state = "ready";

    auth.clearSession();

    expect(store.items).toEqual([]);
    expect(store.resolved).toEqual([]);
    expect(store.activeCount).toBe(0);
    expect(store.severity).toBe("");
    expect(store.resourceType).toBe("");
    expect(store.state).toBe("idle");
  });

  it("drops the previous grant's rows on an auth or not-found error", async () => {
    const store = useIncidentsStore();
    store.items = [incident("old")];
    store.resolved = [incident("old-resolved", { lifecycle: "resolved" })];
    store.resolvedLoaded = true;
    store.activeCount = 1;
    api.listIncidents.mockRejectedValueOnce({ status: 403, message: "forbidden" });

    await store.refresh();

    expect(store.items).toEqual([]);
    expect(store.resolved).toEqual([]);
    expect(store.activeCount).toBe(0);
    expect(store.resolvedLoaded).toBe(false);
    expect(store.state).toBe("error");
  });

  it("keeps node grouping, coverage and hold reason after acknowledge", async () => {
    const store = useIncidentsStore();
    const parent = incident("node-1", {
      scope: "node",
      resource_type: "node",
      severity: "critical",
      coverage: "stale",
    });
    const child = incident("child-1", {
      grouped_under: "node-1",
      coverage: "unknown",
      delivery_hold: "node unavailable",
    });
    store.items = [parent, child];
    api.acknowledgeIncident.mockResolvedValueOnce({
      ...child,
      grouped_under: undefined,
      coverage: "fresh",
      delivery_hold: "acknowledged",
      acknowledged_at: "2026-01-01T00:02:00Z",
    });

    await store.acknowledge("child-1");

    const updated = store.items.find((item) => item.id === "child-1");
    expect(updated?.acknowledged_at).toBe("2026-01-01T00:02:00Z");
    expect(updated?.grouped_under).toBe("node-1");
    expect(updated?.coverage).toBe("unknown");
    expect(updated?.delivery_hold).toBe("node unavailable");
    expect(groupIncidents(store.items)[0]?.children.map((item) => item.id)).toEqual([
      "child-1",
    ]);
  });

  it("shows loading only on the first load and on a filter change", async () => {
    const store = useIncidentsStore();
    const first = deferred<IncidentPage>();
    api.listIncidents.mockReturnValueOnce(first.promise);

    const initial = store.refresh();
    expect(store.loading).toBe(true);
    first.resolve(page([]));
    await initial;
    expect(store.state).toBe("ready");
    expect(store.loading).toBe(false);

    const background = deferred<IncidentPage>();
    api.listIncidents.mockReturnValueOnce(background.promise);
    const poll = store.refresh();
    expect(store.loading).toBe(false);
    background.resolve(page([]));
    await poll;
    expect(store.loading).toBe(false);

    const filtered = deferred<IncidentPage>();
    api.listIncidents.mockReturnValueOnce(filtered.promise);
    store.setSeverity("critical");
    expect(store.loading).toBe(true);
    filtered.resolve(page([]));
    await vi.waitFor(() => expect(store.loading).toBe(false));
    expect(store.state).toBe("ready");
  });

  it("drops a load-more page that a concurrent refresh superseded", async () => {
    const store = useIncidentsStore();
    let resolveStale: (_value: IncidentPage) => void = () => undefined;
    const stale = new Promise<IncidentPage>((resolve) => {
      resolveStale = resolve;
    });
    api.listIncidents
      .mockResolvedValueOnce(page([incident("first")], "cursor-1"))
      .mockImplementationOnce(() => stale)
      .mockResolvedValueOnce(page([incident("fresh")]));

    await store.refresh();
    const more = store.loadMore();
    await store.refresh();
    resolveStale(page([incident("stale")]));
    await more;

    expect(store.items.map((item) => item.id)).toEqual(["fresh"]);
  });

  it("requests enough rows to keep the loaded pages on a live refresh", async () => {
    const store = useIncidentsStore();
    const loaded = Array.from({ length: 40 }, (_, index) => incident(`row-${index}`));
    store.items = loaded;
    store.nextCursor = "cursor-2";
    api.listIncidents.mockResolvedValueOnce(page(loaded, "", 40));

    await store.refresh();

    expect(api.listIncidents).toHaveBeenLastCalledWith(
      expect.objectContaining({ limit: 40 }),
    );
    expect(store.items).toHaveLength(40);
  });
});
