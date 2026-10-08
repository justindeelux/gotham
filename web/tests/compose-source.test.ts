// GS-8 compose source: schema gates, wizard Source step and detail editor.
import { mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick, ref } from "vue";

import {
  composeContentSchema,
  composeFileSchema,
  composeHostPortSchema,
  composeModeSchema,
  composeServiceSchema,
  sourceTypeImplemented,
} from "@/features/applications/schemas/applications";
import { useCreateAppWizard } from "@/features/applications/composables/useCreateAppWizard";
import { useComposeEditor } from "@/features/applications/composables/useComposeEditor";
import { useApplicationsStore } from "@/features/applications/stores/applications";
import { extractComposeServiceNames } from "@/features/applications/utils/compose";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";
import { i18n } from "@/shared/i18n";

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

describe("compose schemas", () => {
  it("enables compose in sourceTypeImplemented", () => {
    expect(sourceTypeImplemented("compose")).toBe(true);
    // All other known types deploy too (compose is the last one, GS-8).
    expect(sourceTypeImplemented("git_private")).toBe(true);
    expect(sourceTypeImplemented("dockerfile")).toBe(true);
    expect(sourceTypeImplemented("image")).toBe(true);
  });

  it("gates the compose mode", () => {
    expect(composeModeSchema.safeParse("paste").success).toBe(true);
    expect(composeModeSchema.safeParse("repo").success).toBe(true);
    expect(composeModeSchema.safeParse("tarball").success).toBe(false);
  });

  it("gates pasted content on non-empty, size and services", () => {
    // Shared table with the server (ValidateComposeContent): both refuse
    // empty, blank, oversize and documents without a services mapping.
    expect(composeContentSchema.safeParse("services:\n  web:\n    image: x\n").success).toBe(true);
    expect(composeContentSchema.safeParse("").success).toBe(false);
    expect(composeContentSchema.safeParse("   \n # only a comment\n").success).toBe(false);
    expect(composeContentSchema.safeParse(`services:\n  web:\n    image: x\n${"x".repeat(256 * 1024)}`).success).toBe(false);
    expect(composeContentSchema.safeParse(`services:\n  web:\n    image: x\n${"x".repeat(256 * 1024 - 40)}`).success).toBe(true);
    // Multi-byte text: under 256K chars but over 256 KiB, matching the server.
    expect(
      composeContentSchema.safeParse(`services:\n${"é".repeat(128 * 1024)}`).success,
    ).toBe(false);
    expect(
      composeContentSchema.safeParse(`services:\n  web:\n    image: x # ${"é".repeat(100)}`).success,
    ).toBe(true);
    expect(composeContentSchema.safeParse("version: \"3\"\n").success).toBe(false);
  });

  it("gates the web service name", () => {
    expect(composeServiceSchema.safeParse("web").success).toBe(true);
    expect(composeServiceSchema.safeParse("api_v2.0-x").success).toBe(true);
    expect(composeServiceSchema.safeParse("").success).toBe(false);
    expect(composeServiceSchema.safeParse("  ").success).toBe(false);
    expect(composeServiceSchema.safeParse("-bad").success).toBe(false);
  });

  it("gates the compose host port above the privileged band", () => {
    expect(composeHostPortSchema.safeParse(null).success).toBe(true);
    expect(composeHostPortSchema.safeParse(0).success).toBe(true);
    expect(composeHostPortSchema.safeParse(8080).success).toBe(true);
    expect(composeHostPortSchema.safeParse(80).success).toBe(false);
    expect(composeHostPortSchema.safeParse(443).success).toBe(false);
    expect(composeHostPortSchema.safeParse(1023).success).toBe(false);
    expect(composeHostPortSchema.safeParse(1024).success).toBe(true);
  });

  it("gates the in-repo file path", () => {
    expect(composeFileSchema.safeParse("docker-compose.yml").success).toBe(true);
    expect(composeFileSchema.safeParse("deploy/compose.yaml").success).toBe(true);
    expect(composeFileSchema.safeParse("").success).toBe(false);
    expect(composeFileSchema.safeParse("/abs.yml").success).toBe(false);
    expect(composeFileSchema.safeParse("../escape.yml").success).toBe(false);
    expect(composeFileSchema.safeParse("a/../../x.yml").success).toBe(false);
    expect(composeFileSchema.safeParse("x".repeat(257)).success).toBe(false);
    // Multi-byte path: under 256 chars but over 256 bytes, matching the server.
    expect(composeFileSchema.safeParse(`${"é".repeat(128)}.yml`).success).toBe(false);
    expect(composeFileSchema.safeParse(`${"é".repeat(100)}.yml`).success).toBe(true);
  });
});

describe("extractComposeServiceNames", () => {
  it("collects the services mapping keys", () => {
    expect(
      extractComposeServiceNames("services:\n  web:\n    image: x\n  worker:\n    image: y\n"),
    ).toEqual(["web", "worker"]);
  });

  it("skips comments, blanks and bodies", () => {
    expect(
      extractComposeServiceNames(
        "# top\nservices: # inline\n\n  web:\n    image: x\n    ports:\n      - 80:80\nvolumes:\n  data: {}\n",
      ),
    ).toEqual(["web"]);
  });

  it("falls back to empty without a mapping", () => {
    expect(extractComposeServiceNames("version: \"3\"\n")).toEqual([]);
    expect(extractComposeServiceNames("services: {}\n")).toEqual([]);
  });
});

function mountWizard() {
  setActivePinia(createPinia());
  let wiz: ReturnType<typeof useCreateAppWizard> | null = null;
  const Harness = defineComponent({
    setup() {
      wiz = useCreateAppWizard(ref(false), (() => undefined) as never);
      return () => h("div");
    },
  });
  const wrapper = mount({ render: () => h(NMessageProvider, null, { default: () => h(Harness) }) });
  return { w: wiz!, wrapper };
}

const reset = {
  sourceType: "git_public",
  providerId: "",
  publicCloneUrl: "",
  repoFullName: "",
  cloneUrl: "",
  composeMode: "paste",
  composeContent: "",
  composeFile: "docker-compose.yml",
  composeService: "",
  branch: "main",
  name: "",
  buildPack: "",
  serverId: "",
  port: 3000,
  hostPort: null,
  baseDomain: "",
  env: [],
  storage: [],
};

const pasted = "services:\n  web:\n    image: example.com/app:1.0\n";

describe("wizard compose source", () => {
  it("gates the Source step on mode, content and service", () => {
    const { w, wrapper } = mountWizard();
    const cases: Array<{ name: string; patch: Record<string, unknown>; valid: boolean }> = [
      { name: "empty-content", patch: { sourceType: "compose", composeMode: "paste", name: "abc" }, valid: false },
      {
        name: "paste-valid",
        patch: { sourceType: "compose", composeMode: "paste", composeContent: pasted, composeService: "web", name: "abc" },
        valid: true,
      },
      {
        name: "paste-no-service",
        patch: { sourceType: "compose", composeMode: "paste", composeContent: pasted, name: "abc" },
        valid: false,
      },
      {
        name: "paste-no-services-key",
        patch: { sourceType: "compose", composeMode: "paste", composeContent: "version: \"3\"\n", composeService: "web", name: "abc" },
        valid: false,
      },
      {
        name: "paste-oversize",
        patch: { sourceType: "compose", composeMode: "paste", composeContent: `${pasted}${"x".repeat(256 * 1024)}`, composeService: "web", name: "abc" },
        valid: false,
      },
      {
        name: "repo-public-valid",
        patch: {
          sourceType: "compose", composeMode: "repo", publicCloneUrl: "https://github.com/o/r.git",
          composeFile: "deploy/compose.yml", composeService: "web", branch: "main", name: "abc",
        },
        valid: true,
      },
      {
        name: "repo-public-bad-url",
        patch: {
          sourceType: "compose", composeMode: "repo", publicCloneUrl: "git@h:o/r.git",
          composeFile: "c.yml", composeService: "web", branch: "main", name: "abc",
        },
        valid: false,
      },
      {
        name: "repo-provider-valid",
        patch: {
          sourceType: "compose", composeMode: "repo", providerId: "p1", repoFullName: "o/r",
          cloneUrl: "git@h:o/r.git", composeFile: "c.yml", composeService: "web", branch: "main", name: "abc",
        },
        valid: true,
      },
      {
        name: "repo-no-file",
        patch: {
          sourceType: "compose", composeMode: "repo", publicCloneUrl: "https://github.com/o/r.git",
          composeFile: "", composeService: "web", branch: "main", name: "abc",
        },
        valid: false,
      },
      {
        name: "repo-no-service",
        patch: {
          sourceType: "compose", composeMode: "repo", publicCloneUrl: "https://github.com/o/r.git",
          composeFile: "c.yml", branch: "main", name: "abc",
        },
        valid: false,
      },
    ];
    for (const c of cases) {
      Object.assign(w.form, { ...reset }, c.patch);
      expect({ name: c.name, actual: w.sourceValid.value }).toEqual({ name: c.name, actual: c.valid });
    }
    wrapper.unmount();
  });

  it("builds the pasted payload without a repository", () => {
    const { w, wrapper } = mountWizard();
    Object.assign(w.form, {
      ...reset,
      sourceType: "compose",
      composeMode: "paste",
      composeContent: pasted,
      composeService: "web",
      branch: "main",
      buildPack: "railpack",
      name: "compose-demo",
    });
    expect(w.sourceValid.value).toBe(true);
    expect(w.buildPayload()).toMatchObject({
      provider: "",
      repo: "",
      clone_url: "",
      source_type: "compose",
      compose_content: pasted,
      compose_service: "web",
      branch: "",
      build_pack: "",
    });
    expect(w.reviewSource.value).toBe("Docker Compose · web");
    wrapper.unmount();
  });

  it("builds the repo payload with file and branch", () => {
    const { w, wrapper } = mountWizard();
    Object.assign(w.form, {
      ...reset,
      sourceType: "compose",
      composeMode: "repo",
      publicCloneUrl: "https://github.com/o/r.git",
      composeFile: "deploy/compose.yml",
      composeService: "web",
      branch: "main",
      name: "compose-demo",
    });
    expect(w.sourceValid.value).toBe(true);
    expect(w.buildPayload()).toMatchObject({
      provider: "",
      repo: "https://github.com/o/r.git",
      clone_url: "https://github.com/o/r.git",
      source_type: "compose",
      compose_file: "deploy/compose.yml",
      compose_service: "web",
      branch: "main",
      build_pack: "",
    });
    expect(w.reviewSource.value).toBe("Docker Compose · web · deploy/compose.yml");
    expect("compose_content" in w.buildPayload()).toBe(false);
    wrapper.unmount();
  });

  it("suggests the web service from pasted text", () => {
    const { w, wrapper } = mountWizard();
    Object.assign(w.form, { ...reset, sourceType: "compose", composeContent: pasted });
    expect(w.composeServiceOptions.value).toEqual([{ label: "web", value: "web" }]);
    expect(w.isCompose.value).toBe(true);
    expect(w.isComposePaste.value).toBe(true);
    w.form.composeMode = "repo";
    expect(w.isComposePaste.value).toBe(false);
    wrapper.unmount();
  });

  it("gates privileged host ports on the Runtime step for compose", () => {
    const { w, wrapper } = mountWizard();
    Object.assign(w.form, {
      ...reset,
      sourceType: "compose",
      serverId: "s1",
      port: 3000,
      hostPort: 80,
      baseDomain: "",
    });
    expect(w.runtimeValid.value).toBe(false);
    w.form.hostPort = 0;
    expect(w.runtimeValid.value).toBe(true);
    w.form.hostPort = 8080;
    expect(w.runtimeValid.value).toBe(true);
    // Other sources keep the existing range.
    w.form.sourceType = "git_public";
    w.form.hostPort = 80;
    expect(w.runtimeValid.value).toBe(true);
    wrapper.unmount();
  });

  it("exposes the compose source option as enabled", () => {
    const { w, wrapper } = mountWizard();
    const options = Object.fromEntries(
      w.sourceTypeOptions.value.map((item) => [item.value, item.disabled === true]),
    );
    expect(options).toEqual({
      git_public: false,
      git_private: false,
      github_app: false,
      gitlab_app: false,
      dockerfile: false,
      compose: false,
      image: false,
    });
    wrapper.unmount();
  });
});

describe("detail compose editor", () => {
  it("seeds from the row and saves content plus service", async () => {
    setActivePinia(createPinia());
    const store = useApplicationsStore();
    const seen: Array<Record<string, unknown>> = [];
    store.update = vi.fn(async (_id: string, input: Record<string, unknown>) => {
      seen.push(input);
      return {} as never;
    }) as never;

    const app = ref({
      id: "app-1",
      source_type: "compose",
      compose_content: pasted,
      compose_file: "",
      compose_service: "web",
      updated_at: "2026-10-08T00:00:00Z",
    } as never);
    const editor = useComposeEditor(app);
    expect(editor.isRepoMode.value).toBe(false);
    expect(editor.content.value).toBe(pasted);
    expect(editor.service.value).toBe("web");
    expect(editor.saveDisabled.value).toBe(false);

    editor.service.value = "";
    expect(editor.saveDisabled.value).toBe(true);
    editor.service.value = "web";
    editor.content.value = "version: \"3\"\n";
    expect(editor.saveDisabled.value).toBe(true);
    editor.content.value = pasted;
    await editor.handleSave();
    expect(seen).toEqual([{ compose_content: pasted, compose_service: "web" }]);

    // An unrelated row bump re-seeds a clean draft but keeps edits.
    app.value = { ...app.value, updated_at: "2026-10-08T00:00:01Z" };
    await nextTick();
    expect(editor.content.value).toBe(pasted);
    editor.content.value = `${pasted}# edited\n`;
    app.value = { ...app.value, updated_at: "2026-10-08T00:00:02Z" };
    await nextTick();
    expect(editor.content.value).toBe(`${pasted}# edited\n`);
  });

  it("saves file plus service for repo-backed rows", async () => {
    setActivePinia(createPinia());
    const store = useApplicationsStore();
    const seen: Array<Record<string, unknown>> = [];
    store.update = vi.fn(async (_id: string, input: Record<string, unknown>) => {
      seen.push(input);
      return {} as never;
    }) as never;

    const app = ref({
      id: "app-1",
      source_type: "compose",
      compose_content: "",
      compose_file: "c.yml",
      compose_service: "web",
      updated_at: "2026-10-08T00:00:00Z",
    } as never);
    const editor = useComposeEditor(app);
    expect(editor.isRepoMode.value).toBe(true);
    expect(editor.saveDisabled.value).toBe(false);
    editor.file.value = "../escape.yml";
    expect(editor.saveDisabled.value).toBe(true);
    editor.file.value = "deploy/c.yml";
    await editor.handleSave();
    expect(seen).toEqual([{ compose_file: "deploy/c.yml", compose_service: "web" }]);
  });
});

describe("compose locales", () => {
  it("resolves the compose strings in English and Vietnamese", () => {
    for (const locale of ["en", "vi"] as const) {
      syncComposerLocale(locale);
      const t = i18n.global.t.bind(i18n.global);
      for (const key of [
        "applications.wizard.sourceCompose",
        "applications.wizard.composeMode",
        "applications.wizard.composeModePaste",
        "applications.wizard.composeModeRepo",
        "applications.wizard.composeContent",
        "applications.wizard.composeFile",
        "applications.wizard.composeService",
        "applications.wizard.hostPortComposeHint",
        "applications.compose.title",
        "applications.compose.save",
        "applications.overview.composeService",
        "applications.rollback.hintCompose",
        "applications.deploymentsTab.footerCompose",
      ]) {
        expect(String(t(key))).not.toBe(key);
      }
    }
    expect(String(i18n.global.t("applications.wizard.sourceCompose"))).not.toContain("sắp có");
  });
});
