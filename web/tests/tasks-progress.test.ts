import { createPinia, setActivePinia } from "pinia";
import { createI18n } from "vue-i18n";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mount } from "@vue/test-utils";

import {
  isTerminalStatus,
  parseTaskFrame,
  taskChannel,
} from "@/features/tasks/api/tasks";
import en from "@/features/tasks/locales/en";
import viCatalog from "@/features/tasks/locales/vi";
import TaskProgressCards from "@/features/tasks/components/TaskProgressCards.vue";
import { useTasksStore } from "@/features/tasks/stores/tasks";
import { checkCatalogParity } from "@/shared/i18n/catalog";

function frameOf(event: Record<string, unknown>): string {
  return JSON.stringify({ channel: "tasks:t1", type: "task", data: JSON.stringify(event) });
}

function runningEvent(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    task_id: "dep-1",
    kind: "deploy",
    name: "web",
    status: "running",
    step: "building",
    progress: 50,
    app_id: "app-1",
    deployment_id: "dep-1",
    server_id: "srv-1",
    project_id: "proj-1",
    environment_id: "env-1",
    team_id: "t1",
    ...overrides,
  };
}

describe("taskChannel", () => {
  it("scopes rooms per team", () => {
    expect(taskChannel("t1")).toBe("tasks:t1");
    expect(taskChannel("t1")).not.toBe(taskChannel("t2"));
  });

  it("classifies terminal states", () => {
    expect(isTerminalStatus("succeeded")).toBe(true);
    expect(isTerminalStatus("failed")).toBe(true);
    expect(isTerminalStatus("running")).toBe(false);
    expect(isTerminalStatus("queued")).toBe(false);
  });
});

describe("parseTaskFrame", () => {
  it("decodes a task event", () => {
    const event = parseTaskFrame(frameOf(runningEvent()));
    expect(event?.taskId).toBe("dep-1");
    expect(event?.step).toBe("building");
    expect(event?.progress).toBe(50);
  });

  it("clamps progress into range", () => {
    expect(parseTaskFrame(frameOf(runningEvent({ progress: 500 })))?.progress).toBe(100);
    expect(parseTaskFrame(frameOf(runningEvent({ progress: -3 })))?.progress).toBe(0);
  });

  it("rejects non-task frames", () => {
    expect(parseTaskFrame(JSON.stringify({ channel: "c", type: "log", data: "x" }))).toBeNull();
    expect(parseTaskFrame(JSON.stringify({ channel: "c", type: "task_snapshot" }))).toBeNull();
    expect(parseTaskFrame("not json")).toBeNull();
    expect(parseTaskFrame(JSON.stringify({ channel: "c", type: "task", data: "{}" }))).toBeNull();
  });
});

describe("tasks locales", () => {
  it("keeps the Vietnamese catalog in parity with English", () => {
    checkCatalogParity(en, viCatalog);
  });
});

describe("useTasksStore", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.useFakeTimers();
  });

  it("upserts cards and auto-dismisses success but keeps failure", () => {
    const store = useTasksStore();
    const running = parseTaskFrame(frameOf(runningEvent()));
    expect(running).not.toBeNull();
    store.applyEvent(running!);
    expect(store.visible).toHaveLength(1);

    const done = parseTaskFrame(frameOf(runningEvent({ status: "succeeded", progress: 100 })));
    store.applyEvent(done!);
    expect(store.visible).toHaveLength(1);
    vi.advanceTimersByTime(8000);
    expect(store.visible).toHaveLength(0);

    const failed = parseTaskFrame(frameOf(runningEvent({ status: "failed", error: "boom" })));
    store.applyEvent(failed!);
    vi.advanceTimersByTime(60_000);
    expect(store.visible).toHaveLength(1);
    expect(store.visible[0].event.error).toBe("boom");
    store.dismiss("dep-1");
    expect(store.visible).toHaveLength(0);
  });

  it("toggles collapse per card", () => {
    const store = useTasksStore();
    store.applyEvent(parseTaskFrame(frameOf(runningEvent()))!);
    expect(store.visible[0].collapsed).toBe(false);
    store.toggleCollapse("dep-1");
    expect(store.visible[0].collapsed).toBe(true);
  });
});

describe("TaskProgressCards", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
  });

  function mountCards(): ReturnType<typeof mount> {
    const i18n = createI18n({
      legacy: false,
      locale: "en",
      messages: { en: { tasks: en } },
    });
    return mount(TaskProgressCards, {
      global: {
        plugins: [i18n],
        stubs: { RouterLink: { template: "<a><slot /></a>" } },
      },
    });
  }

  it("renders nothing without cards and shows name, step and logs link", () => {
    const wrapper = mountCards();
    expect(wrapper.find(".task-cards").exists()).toBe(false);
    const store = useTasksStore();
    store.applyEvent(parseTaskFrame(frameOf(runningEvent()))!);
    return wrapper.vm.$nextTick().then(() => {
      expect(wrapper.find('[data-task-id="dep-1"]').exists()).toBe(true);
      expect(wrapper.text()).toContain("web");
      expect(wrapper.text()).toContain("building");
      expect(wrapper.text()).toContain("View logs");
    });
  });

  it("keeps failed cards with their error", () => {
    const wrapper = mountCards();
    const store = useTasksStore();
    store.applyEvent(parseTaskFrame(frameOf(runningEvent({ status: "failed", error: "boom" })))!);
    return wrapper.vm.$nextTick().then(() => {
      expect(wrapper.find('[data-task-status="failed"]').exists()).toBe(true);
      expect(wrapper.text()).toContain("boom");
    });
  });
});
