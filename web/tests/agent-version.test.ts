import { describe, expect, it } from "vitest";

import type { Server } from "@/features/servers/api/servers";
import {
  agentUpdateInFlight,
  agentVersionMismatch,
  needsAgentUpdate,
  versionsMatch,
} from "@/features/servers/utils/serverListView";

function server(overrides: Partial<Server> = {}): Server {
  return {
    id: "server-1",
    name: "edge-1",
    ip: "203.0.113.10",
    port: 22,
    ssh_user: "root",
    ssh_key_id: null,
    has_password: false,
    status: "ready",
    node_id: "edge-1",
    os: "Debian",
    docker_version: "27.0",
    arch: "amd64",
    total_mem: 1024,
    total_disk: 1024,
    cpu_usage: 0,
    mem_usage: 0,
    disk_usage: 0,
    container_count: 0,
    last_seen: "2026-01-01T00:00:00Z",
    agent_version: "v0.3.2",
    agent_version_reported_at: "2026-01-01T00:00:00Z",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("agent version matching", () => {
  it("treats a leading v and build metadata as the same release", () => {
    expect(versionsMatch("v0.3.3", "0.3.3")).toBe(true);
    expect(versionsMatch("0.3.3+build.7", "v0.3.3")).toBe(true);
    expect(versionsMatch("v0.3.2", "v0.3.3")).toBe(false);
    expect(versionsMatch("", "")).toBe(false);
  });

  it("flags a node whose agent does not match the control plane", () => {
    expect(agentVersionMismatch(server(), "v0.3.3")).toBe(true);
    expect(agentVersionMismatch(server({ agent_version: "v0.3.3" }), "0.3.3")).toBe(false);
    expect(agentVersionMismatch(server({ agent_version: null }), "v0.3.3")).toBe(false);
  });

  it("offers an update only when the node is behind an available release", () => {
    expect(needsAgentUpdate(server(), "v0.3.3")).toBe(true);
    expect(needsAgentUpdate(server({ agent_version: "v0.3.3" }), "v0.3.3")).toBe(false);
    expect(needsAgentUpdate(server({ agent_version: null }), "v0.3.3")).toBe(true);
    expect(needsAgentUpdate(server(), null)).toBe(false);
    expect(needsAgentUpdate(server({ node_id: null }), "v0.3.3")).toBe(false);
  });

  it("only blocks the update action while the control plane reports updating", () => {
    expect(agentUpdateInFlight("updating")).toBe(true);
    expect(agentUpdateInFlight("pending")).toBe(false);
    expect(agentUpdateInFlight("failed")).toBe(false);
    expect(agentUpdateInFlight("up_to_date")).toBe(false);
    expect(agentUpdateInFlight(undefined)).toBe(false);
  });
});
