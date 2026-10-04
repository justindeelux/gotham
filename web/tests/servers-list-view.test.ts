// Unit tests for the servers list view helpers (JUS-24 split of ServersPage).

import { describe, expect, it } from "vitest";

import type { Server } from "../src/features/servers/api/servers";
import {
  containerLabel,
  initials,
  keyLabel,
  matchesFilter,
  matchesSearch,
  needsAgentUpdate,
  nodeMeta,
} from "../src/features/servers/utils/serverListView";

function server(overrides: Partial<Server> = {}): Server {
  return {
    id: "srv-1",
    name: "build-node-03",
    ip: "203.0.113.90",
    port: 22,
    ssh_user: "root",
    ssh_key_id: null,
    has_password: false,
    status: "ready",
    node_id: null,
    os: "Ubuntu 24.04",
    docker_version: "26.1",
    arch: "amd64",
    total_mem: null,
    total_disk: null,
    cpu_usage: null,
    mem_usage: null,
    disk_usage: null,
    container_count: null,
    last_seen: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("needsAgentUpdate", () => {
  it("reports false until the backend provides an agent-version signal", () => {
    expect(needsAgentUpdate(server())).toBe(false);
  });
});

describe("matchesFilter", () => {
  it("matches ready and offline chips by status", () => {
    expect(matchesFilter(server({ status: "ready" }), "ready")).toBe(true);
    expect(matchesFilter(server({ status: "offline" }), "ready")).toBe(false);
    expect(matchesFilter(server({ status: "offline" }), "offline")).toBe(true);
    expect(matchesFilter(server({ status: "ready" }), "offline")).toBe(false);
  });

  it("passes every server through the all chip", () => {
    expect(matchesFilter(server({ status: "error" }), "all")).toBe(true);
  });
});

describe("matchesSearch", () => {
  it("matches name, IP and OS case-insensitively", () => {
    const node = server();
    expect(matchesSearch(node, "build-node")).toBe(true);
    expect(matchesSearch(node, "203.0.113")).toBe(true);
    expect(matchesSearch(node, "ubuntu")).toBe(true);
    expect(matchesSearch(node, "debian")).toBe(false);
  });

  it("treats a blank query as a match", () => {
    expect(matchesSearch(server(), "   ")).toBe(true);
  });
});

describe("initials", () => {
  it("builds the avatar label from up to two words", () => {
    expect(initials("gotham-prod-01")).toBe("GP");
    expect(initials("node")).toBe("N");
  });
});

describe("keyLabel", () => {
  it("names the credential without revealing secrets", () => {
    expect(keyLabel(server({ ssh_key_id: "12345678-uuid" }))).toBe("12345678");
    expect(keyLabel(server({ has_password: true }))).toBe("password stored");
    expect(keyLabel(server())).toBe("no credentials");
  });
});

describe("containerLabel", () => {
  it("keeps an unknown count distinct from zero", () => {
    expect(containerLabel(server({ container_count: null }))).toBe("—");
    expect(containerLabel(server({ container_count: 0 }))).toBe("0 containers");
    expect(containerLabel(server({ container_count: 1 }))).toBe("1 container");
    expect(containerLabel(server({ container_count: 3 }))).toBe("3 containers");
  });
});

describe("nodeMeta", () => {
  it("shows a non-default SSH port with the address", () => {
    expect(nodeMeta(server())).toBe("203.0.113.90 · Ubuntu 24.04 · amd64");
    expect(nodeMeta(server({ port: 2222, os: null, arch: null }))).toBe(
      "203.0.113.90:2222 · — · —",
    );
  });
});
