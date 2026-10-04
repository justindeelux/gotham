// Unit tests for the server rail view helpers (JUS-24 split of ServerRail).

import { describe, expect, it } from "vitest";

import type { Server } from "../src/features/servers/api/servers";
import {
  alertsLabel,
  countAlerts,
  serverInitials,
  serverTip,
  statusDots,
  statusLabels,
} from "../src/features/servers/utils/serverRailView";

function server(name: string, status: Server["status"]): Server {
  return {
    id: name,
    name,
    ip: "10.0.0.1",
    port: 22,
    ssh_user: "root",
    ssh_key_id: null,
    has_password: false,
    status,
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
  };
}

describe("serverInitials", () => {
  it("derives a two-letter avatar from the name", () => {
    expect(serverInitials("build-node-03")).toBe("BN");
    expect(serverInitials("node")).toBe("NO");
    expect(serverInitials("---")).toBe("?");
  });
});

describe("serverTip", () => {
  it("combines the name with the English status label", () => {
    expect(serverTip(server("web-1", "ready"))).toBe("web-1 · Ready");
    expect(serverTip(server("db-1", "offline"))).toBe("db-1 · Offline");
  });
});

describe("status maps", () => {
  it("covers every server status", () => {
    for (const status of ["ready", "validating", "pending", "offline", "error"] as const) {
      expect(statusDots[status]).toBeTruthy();
      expect(statusLabels[status]).toBeTruthy();
    }
  });
});

describe("alerts", () => {
  it("counts offline and error servers", () => {
    const servers = [
      server("a", "ready"),
      server("b", "offline"),
      server("c", "error"),
      server("d", "pending"),
    ];
    expect(countAlerts(servers)).toBe(2);
  });

  it("renders the English alert copy", () => {
    expect(alertsLabel(0)).toBe("No new alerts");
    expect(alertsLabel(1)).toBe("1 new alert");
    expect(alertsLabel(3)).toBe("3 new alerts");
  });
});
