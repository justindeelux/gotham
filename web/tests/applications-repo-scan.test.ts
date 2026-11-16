import { mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick, ref } from "vue";

import { scanRepository } from "@/features/applications/api/applications";
import { useCreateAppWizard } from "@/features/applications/composables/useCreateAppWizard";
import { useAuthStore } from "@/features/auth";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

vi.mock("@/features/applications/api/applications", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/applications/api/applications")>();
  return { ...actual, scanRepository: vi.fn() };
});

const scanned = vi.mocked(scanRepository);

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  scanned.mockReset();
});

afterEach(() => {
  vi.useRealTimers();
});

function seedAuth(): void {
  setActivePinia(createPinia());
  const auth = useAuthStore();
  auth.accessToken = "tok";
  auth.user = {
    id: "u-1",
    email: "owner@example.com",
    role: "owner",
    created_at: "2026-01-01T00:00:00Z",
  };
}

function mountWizard() {
  seedAuth();
  let wizard: ReturnType<typeof useCreateAppWizard> | null = null;
  const Harness = defineComponent({
    setup() {
      wizard = useCreateAppWizard(ref(false), (() => undefined) as never);
      return () => h("div");
    },
  });
  const wrapper = mount({
    render: () => h(NMessageProvider, null, { default: () => h(Harness) }),
  });
  return { w: wizard!, wrapper };
}

async function settle(): Promise<void> {
  await nextTick();
  await vi.advanceTimersByTimeAsync(600);
}

describe("repository scan preselects the build pack", () => {
  it("selects Docker Compose with the scanned file and services", async () => {
    vi.useFakeTimers();
    const { w, wrapper } = mountWizard();
    scanned.mockResolvedValue({
      detected_build_pack: "docker_compose",
      marker_file: "docker-compose.yml",
      compose_files: ["docker-compose.yml", "compose.yaml"],
      compose_file: "docker-compose.yml",
      compose_services: ["web", "worker"],
    });

    w.form.publicCloneUrl = "https://github.com/acme/demo.git";
    await settle();

    expect(scanned).toHaveBeenCalledWith(
      expect.objectContaining({
        source_type: "git_public",
        clone_url: "https://github.com/acme/demo.git",
        branch: "main",
      }),
    );
    expect(w.form.buildPack).toBe("docker_compose");
    expect(w.form.composeFile).toBe("docker-compose.yml");
    expect(w.form.composeService).toBe("");
    expect(w.scan.markerFile).toBe("docker-compose.yml");
    expect(w.scan.composeServices).toEqual(["web", "worker"]);
    wrapper.unmount();
  });

  it("fills the only scanned service and leaves the reason visible", async () => {
    vi.useFakeTimers();
    const { w, wrapper } = mountWizard();
    scanned.mockResolvedValue({
      detected_build_pack: "docker_compose",
      marker_file: "compose.yaml",
      compose_files: ["compose.yaml"],
      compose_file: "compose.yaml",
      compose_services: ["web"],
    });

    w.form.publicCloneUrl = "https://github.com/acme/demo.git";
    await settle();

    expect(w.form.buildPack).toBe("docker_compose");
    expect(w.form.composeFile).toBe("compose.yaml");
    expect(w.form.composeService).toBe("web");
    expect(w.scan.ran).toBe(true);
    expect(w.scan.failed).toBe(false);
    wrapper.unmount();
  });

  it("preselects a language marker as Railpack", async () => {
    vi.useFakeTimers();
    const { w, wrapper } = mountWizard();
    scanned.mockResolvedValue({
      detected_build_pack: "railpack",
      marker_file: "package.json",
      compose_files: [],
      compose_file: "",
      compose_services: [],
    });

    w.form.publicCloneUrl = "https://github.com/acme/demo.git";
    await settle();

    expect(w.form.buildPack).toBe("railpack");
    expect(w.scan.markerFile).toBe("package.json");
    wrapper.unmount();
  });
});

describe("manual build-pack choice wins", () => {
  it("keeps the manual pack when a later scan finds another one", async () => {
    vi.useFakeTimers();
    const { w, wrapper } = mountWizard();
    scanned.mockResolvedValue({
      detected_build_pack: "docker_compose",
      marker_file: "docker-compose.yml",
      compose_files: ["docker-compose.yml"],
      compose_file: "docker-compose.yml",
      compose_services: ["web"],
    });

    w.form.publicCloneUrl = "https://github.com/acme/demo.git";
    await settle();
    expect(w.form.buildPack).toBe("docker_compose");

    w.form.buildPack = "railpack";
    w.markBuildPackTouched();
    w.form.publicCloneUrl = "https://github.com/acme/other.git";
    await settle();

    expect(scanned).toHaveBeenCalledTimes(2);
    expect(w.form.buildPack).toBe("railpack");
    expect(w.scan.markerFile).toBe("docker-compose.yml");
    wrapper.unmount();
  });
});

describe("a failed scan never blocks creation", () => {
  it("shows the hint and leaves the pack manual", async () => {
    vi.useFakeTimers();
    const { w, wrapper } = mountWizard();
    scanned.mockRejectedValue(new Error("boom"));

    w.form.publicCloneUrl = "https://github.com/acme/demo.git";
    await settle();

    expect(w.scan.failed).toBe(true);
    expect(w.scan.ran).toBe(false);
    expect(w.form.buildPack).toBe("");
    w.step.value = 2;
    expect(w.canContinue.value).toBe(true);
    wrapper.unmount();
  });

  it("treats a soft failure from the server the same way", async () => {
    vi.useFakeTimers();
    const { w, wrapper } = mountWizard();
    scanned.mockResolvedValue({
      detected_build_pack: "",
      marker_file: "",
      compose_files: [],
      compose_file: "",
      compose_services: [],
      error: "git clone failed",
    });

    w.form.publicCloneUrl = "https://github.com/acme/demo.git";
    await settle();

    expect(w.scan.failed).toBe(true);
    expect(w.form.buildPack).toBe("");
    wrapper.unmount();
  });
});

describe("private git scans carry the inline credential", () => {
  it("sends the https token the wizard already collected", async () => {
    vi.useFakeTimers();
    const { w, wrapper } = mountWizard();
    scanned.mockResolvedValue({
      detected_build_pack: "dockerfile",
      marker_file: "Dockerfile",
      compose_files: [],
      compose_file: "",
      compose_services: [],
    });

    w.form.sourceType = "git_private";
    await nextTick();
    w.form.privateCloneUrl = "https://git.example.com/acme/demo.git";
    w.form.privateAuth = "https";
    w.form.httpsUsername = "ada";
    w.form.httpsToken = "tok-123";
    await settle();

    expect(scanned).toHaveBeenCalledWith(
      expect.objectContaining({
        source_type: "git_private",
        clone_url: "https://git.example.com/acme/demo.git",
        credential_username: "ada",
        credential_token: "tok-123",
      }),
    );
    expect(w.form.buildPack).toBe("dockerfile");
    wrapper.unmount();
  });
});

describe("scan requests stay debounced", () => {
  it("sends one request for a burst of edits", async () => {
    vi.useFakeTimers();
    const { w, wrapper } = mountWizard();
    scanned.mockResolvedValue({
      detected_build_pack: "",
      marker_file: "",
      compose_files: [],
      compose_file: "",
      compose_services: [],
    });

    w.form.publicCloneUrl = "https://github.com/acme/d";
    await nextTick();
    w.form.publicCloneUrl = "https://github.com/acme/de";
    await nextTick();
    w.form.publicCloneUrl = "https://github.com/acme/demo.git";
    await settle();

    expect(scanned).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });
});
