// PE-5 (JUS-34) environment surface: the resources envelope (API path plus
// warn-only parsing), the unified table builders, and the shared server
// picker options. Pure helpers are pinned row by row; the envelope test pins
// the contract path and the previews flag.
import { describe, expect, it, vi } from "vitest";

vi.mock("@/shared/api/http", () => ({
  http: { get: vi.fn(), post: vi.fn(), patch: vi.fn(), delete: vi.fn() },
  teamHeaders: (teamId: string) =>
    teamId ? { "X-Team-Id": teamId } : {},
}));

import { http } from "@/shared/api/http";
import {
  getEnvironmentResources,
  parseEnvironmentResources,
} from "@/features/projects";
import {
  applicationStatusView,
  applicationSubtitle,
  buildEnvironmentRows,
  databaseSubtitle,
  databaseStatusView,
  filterEnvironmentRows,
  serviceSubtitle,
} from "@/features/projects/composables/useEnvironmentPage";
import type { EnvironmentResources } from "@/features/projects";
import {
  buildServerOptions,
  isUsableServer,
  singleUsableServerId,
  unusableServerHint,
} from "@/features/projects/utils/serverOptions";
import type { Server } from "@/features/servers";

const counts = { applications: 1, services: 1, databases: 1 };

function envelope(overrides = {}) {
  return {
    environment: {
      id: "22222222-2222-4222-8222-222222222222",
      project_id: "11111111-1111-4111-8111-111111111111",
      name: "production",
      created_at: "2026-10-01T00:00:00Z",
      updated_at: "2026-10-01T00:00:00Z",
      resource_counts: counts,
    },
    project: {
      id: "11111111-1111-4111-8111-111111111111",
      name: "storefront",
      description: "",
      created_at: "2026-10-01T00:00:00Z",
      updated_at: "2026-10-02T00:00:00Z",
      environment_count: 1,
      resource_counts: counts,
    },
    applications: [
      {
        id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
        name: "storefront",
        environment_id: "22222222-2222-4222-8222-222222222222",
        environment_name: "production",
        project_id: "11111111-1111-4111-8111-111111111111",
        project_name: "storefront",
        provider: "public",
        repo: "medusajs/medusa",
        clone_url: "https://github.com/medusajs/medusa.git",
        branch: "main",
        build_pack: "",
        base_domain: "",
        base_domain_disabled: false,
        port: 3000,
        host_port: 0,
        server_id: "33333333-3333-4333-8333-333333333333",
        server_name: "prod-01",
        created_at: "2026-10-01T00:00:00Z",
        updated_at: "2026-10-02T00:00:00Z",
      },
    ],
    services: [
      {
        id: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
        name: "plausible",
        status: "running",
        server_id: "33333333-3333-4333-8333-333333333333",
        server_name: "prod-01",
        environment_id: "22222222-2222-4222-8222-222222222222",
        environment_name: "production",
        project_id: "11111111-1111-4111-8111-111111111111",
        project_name: "storefront",
        compose_project: "gotham-bbbbbbbb",
        env: {},
        domains: [{ service: "web", domain: "stats.example.com", port: 8000 }],
        created_at: "2026-10-01T00:00:00Z",
        updated_at: "2026-10-02T00:00:00Z",
      },
    ],
    databases: [
      {
        id: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
        name: "pg-orders",
        environment_id: "22222222-2222-4222-8222-222222222222",
        environment_name: "production",
        project_id: "11111111-1111-4111-8111-111111111111",
        project_name: "storefront",
        engine: "postgres",
        version: "16-alpine",
        status: "running",
        server_id: "33333333-3333-4333-8333-333333333333",
        server_name: "prod-01",
        public_port: 0,
        volume: "gotham-db-cccccccc",
        created_at: "2026-10-01T00:00:00Z",
        updated_at: "2026-10-02T00:00:00Z",
      },
    ],
    ...overrides,
  };
}

function server(overrides: Partial<Server> = {}): Server {
  return {
    id: "33333333-3333-4333-8333-333333333333",
    name: "prod-01",
    ip: "10.0.0.1",
    port: 22,
    ssh_user: "root",
    ssh_key_id: null,
    has_password: false,
    status: "ready",
    node_id: null,
    os: null,
    docker_version: null,
    arch: null,
    total_mem: null,
    total_disk: null,
    cpu_usage: null,
    mem_usage: null,
    disk_usage: null,
    container_count: null,
    last_seen: null,
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    ...overrides,
  } as Server;
}

describe("environment resources api", () => {
  it("reads the envelope with the team header, previews off by default", async () => {
    vi.mocked(http.get).mockResolvedValue({ data: envelope() });
    const resources = await getEnvironmentResources(
      "team-1",
      "22222222-2222-4222-8222-222222222222",
    );
    expect(http.get).toHaveBeenCalledWith(
      "/environments/22222222-2222-4222-8222-222222222222/resources",
      { headers: { "X-Team-Id": "team-1" }, params: {} },
    );
    expect(resources.environment.name).toBe("production");
    expect(resources.applications).toHaveLength(1);
    expect(resources.services[0]!.compose_project).toBe("gotham-bbbbbbbb");
  });

  it("passes previews=1 when the switch is on", async () => {
    vi.mocked(http.get).mockResolvedValue({ data: envelope() });
    await getEnvironmentResources(
      "team-1",
      "22222222-2222-4222-8222-222222222222",
      true,
    );
    expect(http.get).toHaveBeenCalledWith(
      "/environments/22222222-2222-4222-8222-222222222222/resources",
      { headers: { "X-Team-Id": "team-1" }, params: { previews: "1" } },
    );
  });

  it("parses the envelope warn-only", () => {
    const parsed = parseEnvironmentResources(envelope());
    expect(parsed.services).toHaveLength(1);
    expect(parsed.databases[0]!.engine).toBe("postgres");
  });
});

describe("applicationStatusView", () => {
  it("maps deploy states onto table copy", () => {
    expect(applicationStatusView("running")).toEqual({ text: "running", tag: "success" });
    expect(applicationStatusView("failed")).toEqual({ text: "failed", tag: "error" });
    expect(applicationStatusView("queued")).toEqual({ text: "deploying", tag: "warning" });
    expect(applicationStatusView("building")).toEqual({ text: "deploying", tag: "warning" });
    expect(applicationStatusView(null)).toEqual({ text: "not deployed", tag: "default" });
    // A failed state read never renders as "not deployed".
    expect(applicationStatusView("unknown")).toEqual({ text: "unknown", tag: "default" });
  });
});

describe("databaseStatusView", () => {
  it("maps lifecycle states onto table copy", () => {
    expect(databaseStatusView("running")).toEqual({ text: "running", tag: "success" });
    expect(databaseStatusView("error")).toEqual({ text: "error", tag: "error" });
    expect(databaseStatusView("creating")).toEqual({ text: "creating", tag: "warning" });
    expect(databaseStatusView("stopped")).toEqual({ text: "stopped", tag: "default" });
  });
});

describe("row subtitles", () => {
  it("renders repo and branch, domains, and engine versions", () => {
    const resources = envelope() as unknown as EnvironmentResources;
    expect(applicationSubtitle(resources.applications[0]!)).toBe("medusajs/medusa · main");
    expect(serviceSubtitle(resources.services[0]!)).toBe("stats.example.com");
    expect(databaseSubtitle(resources.databases[0]!)).toBe("postgres:16-alpine");
  });

  it("falls back honestly on empty fields", () => {
    const resources = envelope({
      applications: [{ ...envelope().applications[0], repo: "", branch: "" }],
      services: [{ ...envelope().services[0], domains: [] }],
      databases: [{ ...envelope().databases[0], version: "" }],
    }) as unknown as EnvironmentResources;
    expect(applicationSubtitle(resources.applications[0]!)).toBe("—");
    expect(serviceSubtitle(resources.services[0]!)).toBe("compose service");
    expect(databaseSubtitle(resources.databases[0]!)).toBe("postgres");
  });
});

describe("buildEnvironmentRows", () => {
  it("flattens every kind with nested detail links", () => {
    const resources = envelope() as unknown as EnvironmentResources;
    const rows = buildEnvironmentRows(
      resources,
      { "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa": "running" },
      false,
      "11111111-1111-4111-8111-111111111111",
      "22222222-2222-4222-8222-222222222222",
    );
    expect(rows).toHaveLength(3);
    expect(rows[0]).toMatchObject({
      kind: "application",
      name: "storefront",
      serverName: "prod-01",
      statusText: "running",
    });
    expect(rows[0]!.to).toEqual({
      name: "application-detail",
      params: {
        projectId: "11111111-1111-4111-8111-111111111111",
        environmentId: "22222222-2222-4222-8222-222222222222",
        id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
      },
    });
    expect(rows[1]).toMatchObject({ kind: "service", statusText: "running" });
    expect(rows[1]!.to.name).toBe("service-detail");
    expect(rows[2]).toMatchObject({ kind: "database", statusText: "running" });
    expect(rows[2]!.to.name).toBe("database-detail");
  });

  it("marks applications unknown when the state read failed", () => {
    const resources = envelope() as unknown as EnvironmentResources;
    const rows = buildEnvironmentRows(resources, {}, true, "p", "e");
    expect(rows[0]!.statusText).toBe("unknown");
  });
});

describe("filterEnvironmentRows", () => {
  it("applies the type tab and the search query", () => {
    const resources = envelope() as unknown as EnvironmentResources;
    const rows = buildEnvironmentRows(resources, {}, false, "p", "e");
    expect(filterEnvironmentRows(rows, "all", "")).toHaveLength(3);
    expect(filterEnvironmentRows(rows, "applications", "")).toHaveLength(1);
    expect(filterEnvironmentRows(rows, "services", "")).toHaveLength(1);
    expect(filterEnvironmentRows(rows, "databases", "")).toHaveLength(1);
    expect(filterEnvironmentRows(rows, "all", "POSTGRES")).toHaveLength(1);
    expect(filterEnvironmentRows(rows, "all", "prod-01")).toHaveLength(3);
    expect(filterEnvironmentRows(rows, "all", "nope")).toHaveLength(0);
  });
});

describe("server picker options", () => {
  it("enables ready nodes and disables the rest", () => {
    const servers = [
      server(),
      server({ id: "s-off", name: "build-02", status: "offline" }),
    ];
    expect(isUsableServer(servers[0]!)).toBe(true);
    expect(isUsableServer(servers[1]!)).toBe(false);
    expect(buildServerOptions(servers)).toEqual([
      { label: "prod-01 · 10.0.0.1", value: "33333333-3333-4333-8333-333333333333", disabled: false },
      { label: "build-02 · 10.0.0.1", value: "s-off", disabled: true },
    ]);
    expect(unusableServerHint(servers)).toBe("build-02 is offline");
    expect(unusableServerHint([servers[0]!])).toBe("");
  });

  it("preselects only a single usable node", () => {
    const ready = server();
    const offline = server({ id: "s-off", name: "build-02", status: "offline" });
    expect(singleUsableServerId([ready])).toBe(ready.id);
    // A lone offline node is never preselected: it cannot be picked.
    expect(singleUsableServerId([offline])).toBe("");
    expect(singleUsableServerId([ready, offline])).toBe(ready.id);
    expect(singleUsableServerId([ready, server({ id: "s-2", name: "prod-02" })])).toBe("");
    expect(singleUsableServerId([])).toBe("");
  });
});
