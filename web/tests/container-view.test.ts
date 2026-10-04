// Unit tests for the containers view helpers (JUS-24 split of ContainersPage).

import { describe, expect, it } from "vitest";

import type { Container } from "../src/features/servers/api/containers";
import {
  countByFilter,
  isRunning,
  matchesFilter,
  stateTagType,
} from "../src/features/servers/utils/containerView";

function row(overrides: Partial<Container> = {}): Container {
  return {
    id: "c1",
    name: "web",
    image: "nginx:latest",
    state: "running",
    status: "Up 2 hours",
    ports: ["80:80"],
    ...overrides,
  };
}

describe("isRunning", () => {
  it("matches the running state case-insensitively with whitespace", () => {
    expect(isRunning("running")).toBe(true);
    expect(isRunning(" Running ")).toBe(true);
    expect(isRunning("exited")).toBe(false);
  });
});

describe("matchesFilter", () => {
  it("splits running from exited containers", () => {
    expect(matchesFilter(row({ state: "running" }), "running")).toBe(true);
    expect(matchesFilter(row({ state: "exited" }), "running")).toBe(false);
    expect(matchesFilter(row({ state: "exited" }), "exited")).toBe(true);
    expect(matchesFilter(row({ state: "running" }), "exited")).toBe(false);
    expect(matchesFilter(row({ state: "paused" }), "all")).toBe(true);
  });
});

describe("stateTagType", () => {
  it("maps raw Docker states onto tag types", () => {
    expect(stateTagType("running")).toBe("success");
    expect(stateTagType("restarting")).toBe("warning");
    expect(stateTagType("paused")).toBe("warning");
    expect(stateTagType("exited")).toBe("error");
    expect(stateTagType("dead")).toBe("error");
    expect(stateTagType("mysterious")).toBe("default");
  });
});

describe("countByFilter", () => {
  it("counts live chip totals from the list", () => {
    const rows = [row({ state: "running" }), row({ state: "exited" }), row({ state: "running" })];
    expect(countByFilter(rows, "all")).toBe(3);
    expect(countByFilter(rows, "running")).toBe(2);
    expect(countByFilter(rows, "exited")).toBe(1);
  });
});
