import { createPinia, setActivePinia } from "pinia";
import { createI18n } from "vue-i18n";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mount } from "@vue/test-utils";

import {
  isTaskSnapshotMarker,
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
    seq: 2,
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
    expect(event?.seq).toBe(2);
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

  it("recognises the snapshot end marker", () => {
    expect(isTaskSnapshotMarker(JSON.stringify({ channel: "tasks:t1", type: "task_snapshot" }))).toBe(true);
    expect(isTaskSnapshotMarker(JSON.stringify({ channel: "tasks:t1", type: "task", data: "{}" }))).toBe(false);
    expect(isTaskSnapshotMarker("not json")).toBe(false);
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

  it("ignores stale frames older than the applied sequence", () => {
    const store = useTasksStore();
    // A live terminal event races ahead of its own snapshot replay: the late
    // queued frame must not resurrect the finished card.
    store.applyEvent(parseTaskFrame(frameOf(runningEvent({ status: "succeeded", seq: 5 })))!);
    store.applyEvent(parseTaskFrame(frameOf(runningEvent({ status: "queued", step: "", seq: 1 })))!);
    expect(store.visible).toHaveLength(1);
    expect(store.visible[0].event.status).toBe("succeeded");
    // Unnumbered legacy frames still apply.
    store.applyEvent(parseTaskFrame(frameOf(runningEvent({ status: "running", step: "building", seq: 0 })))!);
    expect(store.visible[0].event.status).toBe("running");
  });

  it("marks cards missing from the snapshot as unknown instead of stuck", () => {
    const store = useTasksStore();
    // Both tasks run; a marker closes the pre-disconnect batch (nothing to
    // reconcile yet) and clears it, like a reconnect does on open.
    store.applyEvent(parseTaskFrame(frameOf(runningEvent({ task_id: "dep-1", seq: 2 })))!);
    store.applyEvent(parseTaskFrame(frameOf(runningEvent({ task_id: "dep-2", seq: 3 })))!);
    store.reconcileSnapshot();
    expect(store.visible.every((card) => !card.unknown)).toBe(true);
    // Reconnect replay mentions only dep-1: dep-2 finished while the socket
    // was down, so the closing marker flags it instead of leaving it stuck.
    store.applyEvent(parseTaskFrame(frameOf(runningEvent({ task_id: "dep-1", seq: 4 })))!);
    store.reconcileSnapshot();
    const byId = Object.fromEntries(store.visible.map((card) => [card.event.taskId, card]));
    expect(byId["dep-1"].unknown).toBe(false);
    expect(byId["dep-2"].unknown).toBe(true);
    expect(byId["dep-2"].event.status).toBe("running");
    // Terminal cards are never reconciled, and each marker resets the batch.
    store.applyEvent(parseTaskFrame(frameOf(runningEvent({ task_id: "dep-3", status: "failed", seq: 1 })))!);
    store.reconcileSnapshot();
    expect(store.visible.find((card) => card.event.taskId === "dep-3")?.unknown).toBe(false);
  });

  it("prunes the sequence table with the cards", () => {
    const store = useTasksStore();
    for (let i = 0; i < 25; i++) {
      store.applyEvent(parseTaskFrame(frameOf(runningEvent({ task_id: `ok-${i}`, status: "succeeded", seq: i + 1 })))!);
      store.applyEvent(parseTaskFrame(frameOf(runningEvent({ task_id: `no-${i}`, status: "failed", seq: i + 1 })))!);
    }
    expect(store.trackedSequences()).toBe(50);
    vi.advanceTimersByTime(8000);
    expect(store.visible.every((card) => card.event.status === "failed")).toBe(true);
    expect(store.trackedSequences()).toBe(25);
    for (const card of [...store.visible]) {
      store.dismiss(card.event.taskId);
    }
    expect(store.visible).toHaveLength(0);
    expect(store.trackedSequences()).toBe(0);
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
      expect(wrapper.text()).toContain("Building");
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

  it("flags cards finished while the socket was down as finished", () => {
    const wrapper = mountCards();
    const store = useTasksStore();
    store.applyEvent(parseTaskFrame(frameOf(runningEvent()))!);
    store.reconcileSnapshot();
    store.reconcileSnapshot();
    return wrapper.vm.$nextTick().then(() => {
      // First marker closes the pre-disconnect batch; the second finds the
      // still-running card absent and marks it finished instead of stuck.
      expect(wrapper.find('[data-task-unknown="true"]').exists()).toBe(true);
      expect(wrapper.text()).toContain("Finished");
    });
  });
});
