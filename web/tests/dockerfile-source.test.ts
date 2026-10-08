// GS-7 Dockerfile source: schema gates, wizard Source step and detail editor.
import { mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, ref } from "vue";

import {
  buildArgKeySchema,
  buildArgValueSchema,
  dockerfileContentSchema,
  sourceTypeImplemented,
} from "@/features/applications/schemas/applications";
import {
  buildArgsPayload,
  buildArgsValid,
  useCreateAppWizard,
} from "@/features/applications/composables/useCreateAppWizard";
import { useDockerfileEditor } from "@/features/applications/composables/useDockerfileEditor";
import { useApplicationsStore } from "@/features/applications/stores/applications";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

describe("dockerfile schemas", () => {
  it("enables dockerfile in sourceTypeImplemented", () => {
    expect(sourceTypeImplemented("dockerfile")).toBe(true);
    expect(sourceTypeImplemented("git_private")).toBe(false);
    expect(sourceTypeImplemented("compose")).toBe(false);
    expect(sourceTypeImplemented("image")).toBe(false);
  });

  it("gates pasted content on non-empty, size and FROM", () => {
    expect(dockerfileContentSchema.safeParse("FROM alpine:3.20\n").success).toBe(true);
    expect(dockerfileContentSchema.safeParse("from scratch\n").success).toBe(true);
    expect(
      dockerfileContentSchema.safeParse("# comment\n\nFROM scratch\n").success,
    ).toBe(true);
    expect(
      dockerfileContentSchema.safeParse("ARG V=1\nFROM alpine:${V}\n").success,
    ).toBe(true);
    expect(dockerfileContentSchema.safeParse("").success).toBe(false);
    expect(dockerfileContentSchema.safeParse("   \n # only a comment\n").success).toBe(false);
    expect(dockerfileContentSchema.safeParse("RUN echo hi\n").success).toBe(false);
    expect(
      dockerfileContentSchema.safeParse(`FROM scratch\n${"x".repeat(64 * 1024)}`).success,
    ).toBe(false);
  });

  it("gates build arg keys and values", () => {
    expect(buildArgKeySchema.safeParse("APP_ENV").success).toBe(true);
    expect(buildArgKeySchema.safeParse("  ").success).toBe(false);
    expect(buildArgValueSchema.safeParse("production").success).toBe(true);
    expect(buildArgValueSchema.safeParse("x".repeat(4 * 1024 + 1)).success).toBe(false);
    expect(buildArgsValid([{ key: "A", value: "1" }])).toBe(true);
    expect(buildArgsValid([{ key: "", value: "" }])).toBe(true);
    expect(buildArgsValid([{ key: "", value: "dropped" }])).toBe(false);
    expect(buildArgsValid([{ key: "A=B", value: "1" }])).toBe(false);
    expect(
      buildArgsPayload([
        { key: " A ", value: "1" },
        { key: "", value: "dropped" },
      ]),
    ).toEqual({ A: "1" });
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
  dockerfileContent: "",
  buildArgs: [],
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

describe("wizard dockerfile source", () => {
  it("gates the Source step on content and build args", () => {
    const { w, wrapper } = mountWizard();
    const cases: Array<{ name: string; patch: Record<string, unknown>; valid: boolean }> = [
      { name: "empty-content", patch: { sourceType: "dockerfile", branch: "main", name: "abc" }, valid: false },
      {
        name: "valid",
        patch: { sourceType: "dockerfile", dockerfileContent: "FROM alpine:3.20\n", branch: "main", name: "abc" },
        valid: true,
      },
      {
        name: "no-from",
        patch: { sourceType: "dockerfile", dockerfileContent: "RUN echo hi\n", branch: "main", name: "abc" },
        valid: false,
      },
      {
        name: "bad-arg-key",
        patch: {
          sourceType: "dockerfile",
          dockerfileContent: "FROM alpine:3.20\n",
          buildArgs: [{ key: "A=B", value: "1" }],
          branch: "main",
          name: "abc",
        },
        valid: false,
      },
      {
        name: "blank-arg-rows-ignored",
        patch: {
          sourceType: "dockerfile",
          dockerfileContent: "FROM alpine:3.20\n",
          buildArgs: [{ key: "", value: "" }],
          branch: "main",
          name: "abc",
        },
        valid: true,
      },
    ];
    for (const c of cases) {
      Object.assign(w.form, { ...reset, buildArgs: [] }, c.patch);
      expect({ name: c.name, actual: w.sourceValid.value }).toEqual({ name: c.name, actual: c.valid });
    }
    wrapper.unmount();
  });

  it("builds the dockerfile payload without a repository", () => {
    const { w, wrapper } = mountWizard();
    Object.assign(w.form, {
      ...reset,
      sourceType: "dockerfile",
      dockerfileContent: "FROM alpine:3.20\n",
      buildArgs: [{ key: "APP_ENV", value: "production" }, { key: "", value: "dropped" }],
      branch: "main",
      name: "docker-demo",
    });
    expect(w.buildPayload()).toMatchObject({
      provider: "",
      repo: "",
      clone_url: "",
      source_type: "dockerfile",
      dockerfile_content: "FROM alpine:3.20\n",
      build_args: { APP_ENV: "production" },
    });
    expect(w.reviewSource.value).toBe("Dockerfile · 1 build args");
    wrapper.unmount();
  });

  it("exposes the dockerfile source option as enabled", () => {
    const { w, wrapper } = mountWizard();
    const option = w.sourceTypeOptions.value.find((item) => item.value === "dockerfile");
    expect(option?.disabled).toBeFalsy();
    expect(w.isDockerfile.value).toBe(false);
    w.form.sourceType = "dockerfile";
    expect(w.isDockerfile.value).toBe(true);
    wrapper.unmount();
  });
});

describe("detail dockerfile editor", () => {
  it("seeds from the row and saves text plus args", async () => {
    setActivePinia(createPinia());
    const store = useApplicationsStore();
    const seen: Array<Record<string, unknown>> = [];
    store.update = vi.fn(async (_id: string, input: Record<string, unknown>) => {
      seen.push(input);
      return {} as never;
    }) as never;

    const app = ref({
      id: "app-1",
      source_type: "dockerfile",
      dockerfile_content: "FROM alpine:3.20\n",
      build_args: { APP_ENV: "production" },
      updated_at: "2026-10-08T00:00:00Z",
    } as never);
    const editor = useDockerfileEditor(app);
    expect(editor.content.value).toBe("FROM alpine:3.20\n");
    expect(editor.args.value).toEqual([{ key: "APP_ENV", value: "production" }]);
    expect(editor.saveDisabled.value).toBe(false);

    editor.content.value = "RUN echo hi\n";
    expect(editor.saveDisabled.value).toBe(true);
    editor.content.value = "FROM alpine:3.21\n";
    editor.args.value.push({ key: "", value: "has value but no key" });
    expect(editor.saveDisabled.value).toBe(true);
    editor.args.value.pop();
    editor.args.value.push({ key: "EXTRA", value: "" });
    await editor.handleSave();
    expect(seen).toEqual([
      {
        dockerfile_content: "FROM alpine:3.21\n",
        build_args: { APP_ENV: "production", EXTRA: "" },
      },
    ]);
  });
});
