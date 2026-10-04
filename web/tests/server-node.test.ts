// Server node card text derivations for the dashboard health grid.
import { describe, expect, it } from "vitest";

import type { Server } from "@/features/servers";
import {
  buildServerNodeCard,
  nodeInitials,
  nodeSubtitle,
} from "@/features/dashboard/utils/serverNode";

function server(overrides: Partial<Server> = {}): Server {
  return {
    id: "srv-1",
    name: "alpha",
    ip: "10.0.0.2",
    port: 9442,
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
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  } as Server;
}

describe("nodeInitials", () => {
  it("derives a two-letter avatar", () => {
    expect(nodeInitials("alpha")).toBe("AL");
    expect(nodeInitials("alpha beta")).toBe("AB");
    expect(nodeInitials("my-node_01")).toBe("MN");
  });
});

describe("nodeSubtitle", () => {
  it("summarizes address, OS, and Docker version", () => {
    expect(nodeSubtitle(server())).toBe("10.0.0.2:9442");
    expect(
      nodeSubtitle(server({ os: "Ubuntu 24.04", docker_version: "27.1" })),
    ).toBe("10.0.0.2:9442 · Ubuntu 24.04 · Docker 27.1");
  });
});

describe("buildServerNodeCard", () => {
  it("maps a server plus metric views into the card model", () => {
    const view = { label: "10%", percentage: 10, color: "red" };
    const card = buildServerNodeCard(
      server({ name: "alpha beta", container_count: 3, arch: "amd64" }),
      { cpu: view, ram: view, disk: view },
    );
    expect(card).toMatchObject({
      id: "srv-1",
      name: "alpha beta",
      initials: "AB",
      subtitle: "10.0.0.2:9442",
      status: "ready",
      cpu: view,
      containerCount: 3,
      arch: "amd64",
      sshUser: "root",
    });
  });
});
