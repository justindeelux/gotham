import { mount } from "@vue/test-utils";
import { beforeAll, describe, expect, it } from "vitest";

import type { Server } from "@/features/servers";
import ServerNodeCard from "@/features/dashboard/components/ServerNodeCard.vue";
import {
  buildServerNodeCard,
  nodeInitials,
  nodeSubtitle,
} from "@/features/dashboard/utils/serverNode";
import { i18n, registerDiscoveredCatalogs } from "@/shared/i18n";

beforeAll(() => {
  registerDiscoveredCatalogs();
});

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
  it("maps a server into the card model", () => {
    const card = buildServerNodeCard(
      server({
        name: "alpha beta",
        container_count: 3,
        arch: "amd64",
        cpu_usage: 0.1,
        mem_usage: 0.2,
        disk_usage: 0.3,
        last_seen: "2026-10-30T10:00:00Z",
      }),
    );
    expect(card).toMatchObject({
      id: "srv-1",
      name: "alpha beta",
      initials: "AB",
      subtitle: "10.0.0.2:9442",
      status: "ready",
      cpuUsage: 0.1,
      memUsage: 0.2,
      diskUsage: 0.3,
      containerCount: 3,
      arch: "amd64",
      sshUser: "root",
      lastSeen: "2026-10-30T10:00:00Z",
    });
  });
});

describe("ServerNodeCard", () => {
  function mountCard(lastSeen: string): ReturnType<typeof mount> {
    return mount(ServerNodeCard, {
      props: {
        card: buildServerNodeCard(
          server({
            cpu_usage: 0.1,
            mem_usage: 0.2,
            disk_usage: 0.3,
            last_seen: lastSeen,
          }),
        ),
      },
      global: {
        plugins: [i18n],
        stubs: { RouterLink: { template: "<a><slot /></a>" } },
      },
    });
  }

  it("marks all three usage meters stale when the node stopped reporting", () => {
    const wrapper = mountCard(
      new Date(Date.now() - 10 * 60_000).toISOString(),
    );
    expect(wrapper.findAll(".usage-row")).toHaveLength(3);
    expect(wrapper.findAll(".usage-row.is-stale")).toHaveLength(3);
    wrapper.unmount();
  });

  it("keeps fresh usage meters live", () => {
    const wrapper = mountCard(new Date().toISOString());
    expect(wrapper.findAll(".usage-row.is-stale")).toHaveLength(0);
    wrapper.unmount();
  });
});
