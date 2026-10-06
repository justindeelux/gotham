// I18N-5 live-switch and raw-behavior checks for servers and dashboard.
//
// Pure helpers stay byte-identical in English when no locale is passed and
// render translated display text for Vietnamese. Raw classifiers
// (isApiError, stripErrorPrefix, conflictDetail) and the describe* detail
// paths keep their exact semantics; only empty fallbacks localize.

import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { computed, defineComponent, h, ref } from "vue";

import {
  describeServerError,
  failureText,
  isApiError,
  stripErrorPrefix,
} from "@/features/servers/api/servers";
import { describeContainerError } from "@/features/servers/api/containers";
import { describeMetricsError } from "@/features/servers/api/metrics";
import { listContainers } from "@/features/servers/api/containers";
import { listServers } from "@/features/servers/api/servers";
import { useContainersPage } from "@/features/servers/composables/useContainersPage";
import { useServersStore } from "@/features/servers/stores/servers";
import type { Server } from "@/features/servers/api/servers";
import { containerLabel, keyLabel } from "@/features/servers/utils/serverListView";
import { authLabel, summaryLine } from "@/features/servers/utils/serverDetailView";
import { stateLabel } from "@/features/servers/utils/containerView";
import { noticeText } from "@/features/servers/utils/logFormat";
import {
  alertsLabel,
  countAlerts,
  serverTip,
  statusDots,
  statusLabelFor,
} from "@/features/servers/utils/serverRailView";
import {
  buildMetricCharts,
  metricRangeHint,
  metricsFailureText,
  refreshMsForChoice,
} from "@/features/servers/utils/serverMetricsView";
import {
  applicationTileView,
  buildApplicationTileInput,
  incompleteTileHint,
} from "@/features/dashboard/utils/dashboard";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";
import enServers from "@/features/servers/locales/en";
import viServers from "@/features/servers/locales/vi";
import enDashboard from "@/features/dashboard/locales/en";
import viDashboard from "@/features/dashboard/locales/vi";

vi.mock("@/features/servers/api/containers", async (importOriginal) => ({
  ...(await importOriginal<
    typeof import("@/features/servers/api/containers")
  >()),
  listContainers: vi.fn(),
}));

vi.mock("@/features/servers/api/servers", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/features/servers/api/servers")>()),
  listServers: vi.fn(),
  getServer: vi.fn().mockRejectedValue(new Error("not found")),
}));

if (!globalThis.window.matchMedia) {
  globalThis.window.matchMedia = (() => ({
    matches: false,
    media: "",
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof globalThis.window.matchMedia;
}

beforeAll(() => {
  registerDiscoveredCatalogs();
});

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
  vi.clearAllMocks();
});

function server(overrides: Partial<Server> = {}): Server {
  return {
    id: "srv-1",
    name: "web-1",
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
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("feature catalog parity", () => {
  it("ships equal en/vi key sets with matching params and valid syntax", () => {
    expect(checkCatalogParity(enServers, viServers)).toEqual([]);
    expect(checkCatalogParity(enDashboard, viDashboard)).toEqual([]);
  });
});

describe("raw classifier invariants", () => {
  it("keeps prefix stripping and ApiError narrowing untouched", () => {
    expect(
      stripErrorPrefix("servers: validation failed: ssh dial timeout"),
    ).toBe("validation failed: ssh dial timeout");
    expect(isApiError({ message: "x", status: 500 })).toBe(true);
    expect(isApiError(new Error("x"))).toBe(false);
    expect(describeServerError({ message: "servers: boom", status: 500 })).toBe("boom");
    expect(describeContainerError({ message: "containers: boom", status: 500 })).toBe(
      "boom",
    );
    expect(describeMetricsError({ message: "ws: boom", status: 500 })).toBe("boom");
  });

  it("localizes only the empty fallbacks in Vietnamese", () => {
    setLocale("vi", null);
    try {
      expect(describeServerError({ message: "", status: 500 })).toBe("Yêu cầu thất bại");
      expect(describeServerError(undefined)).toBe("Đã xảy ra lỗi. Vui lòng thử lại.");
      expect(describeMetricsError({ message: "", status: 401 })).toBe(
        "Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.",
      );
      expect(describeServerError({ message: "servers: boom", status: 500 })).toBe(
        "boom",
      );
    } finally {
      setLocale("en", null);
    }
  });
});

describe("servers display helpers", () => {
  it("renders English by default and Vietnamese on request", () => {
    const keyed = server({ ssh_key_id: "12345678-uuid" });
    expect(keyLabel(keyed)).toBe("12345678");
    expect(keyLabel(server({ has_password: true }))).toBe("password stored");
    expect(keyLabel(server({ has_password: true }), "vi")).toBe("đã lưu mật khẩu");
    expect(keyLabel(server(), "vi")).toBe("chưa có thông tin đăng nhập");
    expect(containerLabel(server({ container_count: 1 }))).toBe("1 container");
    expect(containerLabel(server({ container_count: 3 }), "vi")).toBe("3 container");
    expect(authLabel(server({ has_password: true }), "vi")).toBe("đã lưu mật khẩu");
    expect(summaryLine(server(), "vi")).toBe(
      "10.0.0.1:22 · Không rõ hệ điều hành · Không rõ kiến trúc · Không rõ Docker",
    );
    expect(stateLabel("", "vi")).toBe("không rõ");
    expect(stateLabel("running", "vi")).toBe("running");
    expect(serverTip(server({ name: "web-1", status: "ready" }), "vi")).toBe(
      "web-1 · Sẵn sàng",
    );
    expect(statusLabelFor("offline", "vi")).toBe("Ngoại tuyến");
    expect(alertsLabel(0, "vi")).toBe("Không có cảnh báo mới");
    expect(alertsLabel(3, "vi")).toBe("3 cảnh báo mới");
    expect(
      noticeText({ raw: "", channel: "c", kind: "notice", receivedAt: 0, payload: {} }, "vi"),
    ).toBe("Luồng nhật ký bị gián đoạn; đang kết nối lại…");
  });

  it("keeps status, count and metric thresholds unchanged", () => {
    expect(countAlerts([server({ status: "offline" }), server({ status: "error" })])).toBe(
      2,
    );
    expect(Object.keys(statusDots).sort()).toEqual(
      ["error", "offline", "pending", "ready", "validating"].sort(),
    );
    expect(refreshMsForChoice("off")).toBe(0);
    expect(refreshMsForChoice("15s")).toBe(15_000);
    expect(metricRangeHint("1m")).toBe("Step 1m · last hour");
    expect(metricRangeHint("1m", "vi")).toBe("Bước 1m · 1 giờ qua");
    const charts = buildMetricCharts([], "vi");
    expect(charts.map((chart) => chart.title)).toEqual([
      "CPU",
      "RAM",
      "Ổ đĩa I/O",
      "Mạng",
    ]);
    expect(buildMetricCharts([]).map((chart) => chart.title)).toEqual([
      "CPU",
      "RAM",
      "Disk I/O",
      "Network",
    ]);
  });
});

describe("dashboard tile helper", () => {
  it("keeps the figure while the incomplete hint follows the locale", () => {
    const input = buildApplicationTileInput({
      loading: false,
      error: null,
      total: 2,
      running: 1,
      failedReads: 1,
    });
    const en = applicationTileView(input);
    expect(en.countText).toBe("≥1/2");
    expect(en.hint).toBe(incompleteTileHint);
    const vi = applicationTileView(input, "vi");
    expect(vi.countText).toBe("≥1/2");
    expect(vi.hint).toBe("Không đọc được một số trạng thái");
  });
});

describe("live locale switch", () => {
  it("re-renders display text without losing selection state", async () => {
    const Harness = defineComponent({
      name: "SwitchHarness",
      setup() {
        // The selected container id lives beside its display label: a
        // language switch must re-render the label without touching it.
        const selected = ref("ctr-9");
        return { selected };
      },
      render() {
        return h("div", [
          h("span", { class: "label" }, containerLabel(server({ container_count: 2 }))),
          h("span", { class: "selected" }, (this as { selected: string }).selected),
        ]);
      },
    });
    const wrapper = mount(Harness, { global: { plugins: [i18n] } });
    try {
      expect(wrapper.find(".label").text()).toBe("2 containers");
      setLocale("vi", null);
      await wrapper.vm.$nextTick();
      expect(wrapper.find(".label").text()).toBe("2 container");
      expect(wrapper.find(".selected").text()).toBe("ctr-9");
    } finally {
      wrapper.unmount();
      setLocale("en", null);
    }
  });
});

describe("retained failure display", () => {
  it("pairs a localized summary with the raw diagnostic", () => {
    const apiError = { message: "servers: validation failed: ssh dial timeout", status: 500 };
    expect(failureText(apiError)).toBe(
      "Request failed — validation failed: ssh dial timeout",
    );
    expect(failureText(apiError, "vi")).toBe(
      "Yêu cầu thất bại — validation failed: ssh dial timeout",
    );
    expect(failureText(new Error("proxy: bad gateway"))).toBe(
      "Something went wrong. Please try again. — bad gateway",
    );
    expect(failureText({ message: "", status: 500 })).toBe("Request failed");
    expect(failureText({ message: "", status: 500 }, "vi")).toBe("Yêu cầu thất bại");
    expect(failureText(undefined)).toBe("Something went wrong. Please try again.");
    expect(failureText(undefined, "vi")).toBe("Đã xảy ra lỗi. Vui lòng thử lại.");
  });

  it("keeps curated metrics summaries while localizing the rest", () => {
    expect(metricsFailureText(null)).toBeNull();
    expect(metricsFailureText({ message: "x", status: 401 })).toBe(
      "Your session expired. Please sign in again.",
    );
    expect(metricsFailureText({ message: "x", status: 401 }, "vi")).toBe(
      "Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.",
    );
    expect(metricsFailureText({ message: "x", status: 404 }, "vi")).toBe(
      "Control plane này chưa bật chỉ số máy chủ (FEATURE_METRICS=false).",
    );
    expect(metricsFailureText({ message: "", status: 400 })).toBe(
      "Invalid metrics range. Pick another range and retry.",
    );
    expect(metricsFailureText({ message: "ws: boom", status: 400 }, "vi")).toBe(
      "Yêu cầu thất bại — boom",
    );
    expect(metricsFailureText({ message: "ws: boom", status: 500 })).toBe(
      "Request failed — boom",
    );
  });

  it("re-renders the retained store banner on switch without refetching", async () => {
    setActivePinia(createPinia());
    const store = useServersStore();
    const apiError = {
      message: "servers: validation failed: ssh dial timeout",
      status: 500,
    };
    vi.mocked(listServers).mockRejectedValueOnce(apiError);
    await expect(store.fetchServers()).rejects.toBe(apiError);
    expect(store.error).toBe("Request failed — validation failed: ssh dial timeout");
    expect(vi.mocked(listServers)).toHaveBeenCalledTimes(1);
    setLocale("vi", null);
    expect(store.error).toBe("Yêu cầu thất bại — validation failed: ssh dial timeout");
    expect(vi.mocked(listServers)).toHaveBeenCalledTimes(1);
    vi.mocked(listServers).mockResolvedValueOnce([]);
    await store.fetchServers();
    expect(store.error).toBeNull();
    expect(store.servers).toEqual([]);
  });

  it("re-renders the containers banner without refetching or losing draft state", async () => {
    const apiError = { message: "containers: dial failed", status: 500 };
    vi.mocked(listContainers).mockRejectedValueOnce(apiError);
    let exposed: ReturnType<typeof useContainersPage> | null = null;
    const Child = defineComponent({
      name: "ContainersProbeChild",
      setup() {
        const page = useContainersPage(computed(() => "srv-1"));
        exposed = page;
        return { page };
      },
      render() {
        const page = (this as { page: ReturnType<typeof useContainersPage> }).page;
        return h("div", [
          h("span", { class: "banner" }, page.error.value ?? ""),
          h("span", { class: "filter" }, page.activeFilter.value),
          h("span", { class: "query" }, page.searchQuery.value),
        ]);
      },
    });
    const Probe = defineComponent({
      name: "ContainersProbe",
      render() {
        return h(NMessageProvider, () => h(Child));
      },
    });
    const wrapper = mount(Probe, { global: { plugins: [i18n] } });
    try {
      await flushPromises();
      await wrapper.vm.$nextTick();
      expect(wrapper.find(".banner").text()).toBe(
        "Request failed — dial failed",
      );
      expect(vi.mocked(listContainers)).toHaveBeenCalledTimes(1);
      if (!exposed) {
        throw new Error("probe page not exposed");
      }
      exposed.searchQuery.value = "ng";
      exposed.activeFilter.value = "running";
      await wrapper.vm.$nextTick();
      setLocale("vi", null);
      await wrapper.vm.$nextTick();
      expect(wrapper.find(".banner").text()).toBe("Yêu cầu thất bại — dial failed");
      expect(vi.mocked(listContainers)).toHaveBeenCalledTimes(1);
      expect(wrapper.find(".query").text()).toBe("ng");
      expect(wrapper.find(".filter").text()).toBe("running");
      expect(exposed.selected.value).toBeNull();
      expect(exposed.drawerOpen.value).toBe(false);
    } finally {
      wrapper.unmount();
      setLocale("en", null);
    }
  });
});
