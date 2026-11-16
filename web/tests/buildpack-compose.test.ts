import { mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";
import { defineComponent, h, nextTick, ref } from "vue";

import { useAuthStore } from "@/features/auth";
import WizardBuildPackStep from "@/features/applications/components/WizardBuildPackStep.vue";
import {
  provideCreateWizard,
  useCreateAppWizard,
} from "@/features/applications/composables/useCreateAppWizard";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

function seedAuth(): void {
  setActivePinia(createPinia());
  const auth = useAuthStore();
  auth.accessToken = "tok";
  auth.user = { id: "u-1", email: "owner@example.com", role: "owner", created_at: "2026-01-01T00:00:00Z" };
}

function mountWizard() {
  seedAuth();
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

async function mountStep() {
  seedAuth();
  let wiz: ReturnType<typeof useCreateAppWizard> | null = null;
  const Harness = defineComponent({
    setup() {
      wiz = useCreateAppWizard(ref(false), (() => undefined) as never);
      provideCreateWizard(wiz);
      return () => h(WizardBuildPackStep);
    },
  });
  const wrapper = mount(
    { render: () => h(NMessageProvider, null, { default: () => h(Harness) }) },
    { global: { plugins: [i18n] } },
  );
  await nextTick();
  return { w: wiz!, wrapper };
}

const reset = {
  sourceType: "git_public",
  publicCloneUrl: "",
  branch: "main",
  buildPack: "",
  composeContent: "",
  composeFile: "docker-compose.yml",
  composeService: "",
  name: "",
  serverId: "",
  env: [],
  storage: [],
};

const composeBase = {
  ...reset,
  sourceType: "git_public",
  publicCloneUrl: "https://github.com/o/r.git",
  branch: "main",
  buildPack: "docker_compose",
  composeFile: "deploy/compose.yml",
  composeService: "web",
  name: "shop",
};

describe("docker compose build pack choice", () => {
  it("is offered for every repository source and hidden elsewhere", () => {
    const { w, wrapper } = mountWizard();
    const values = (): string[] => w.buildPackChoices.value.map((choice) => choice.value);
    for (const source of ["git_public", "git_private", "github_app", "gitlab_app"]) {
      w.form.sourceType = source as never;
      expect({ source, has: values().includes("docker_compose") }).toEqual({ source, has: true });
    }
    for (const source of ["dockerfile", "image", "compose"]) {
      w.form.sourceType = source as never;
      expect({ source, has: values().includes("docker_compose") }).toEqual({ source, has: false });
    }
    wrapper.unmount();
  });

  it("requires the compose file and service before continuing", () => {
    const { w, wrapper } = mountWizard();
    Object.assign(w.form, composeBase);
    expect(w.buildPackValid.value).toBe(true);

    w.form.composeFile = "";
    expect(w.buildPackValid.value).toBe(false);
    w.form.composeFile = "/abs/compose.yml";
    expect(w.buildPackValid.value).toBe(false);
    w.form.composeFile = "../escape.yml";
    expect(w.buildPackValid.value).toBe(false);
    w.form.composeFile = "docker-compose.yml";
    expect(w.buildPackValid.value).toBe(true);

    w.form.composeService = "";
    expect(w.buildPackValid.value).toBe(false);
    w.form.composeService = "-bad";
    expect(w.buildPackValid.value).toBe(false);
    w.form.composeService = "api_v2.0-x";
    expect(w.buildPackValid.value).toBe(true);
    wrapper.unmount();
  });

  it("sends the compose fields for a public git repository", () => {
    const { w, wrapper } = mountWizard();
    Object.assign(w.form, composeBase);
    expect(w.buildPayload()).toMatchObject({
      source_type: "git_public",
      repo: "https://github.com/o/r.git",
      clone_url: "https://github.com/o/r.git",
      branch: "main",
      build_pack: "docker_compose",
      compose_file: "deploy/compose.yml",
      compose_service: "web",
    });
    expect("compose_content" in w.buildPayload()).toBe(false);
    wrapper.unmount();
  });

  it("keeps the provider source fields for a GitHub App app", () => {
    const { w, wrapper } = mountWizard();
    Object.assign(w.form, composeBase, {
      sourceType: "github_app",
      providerId: "42",
      repoFullName: "owner/repo",
      cloneUrl: "git@github.com:owner/repo.git",
    });
    expect(w.buildPayload()).toMatchObject({
      source_type: "github_app",
      provider: "github",
      github_app_id: "42",
      repo: "owner/repo",
      clone_url: "git@github.com:owner/repo.git",
      build_pack: "docker_compose",
      compose_file: "deploy/compose.yml",
      compose_service: "web",
    });
    wrapper.unmount();
  });

  it("leaves the other packs without compose fields", () => {
    const { w, wrapper } = mountWizard();
    Object.assign(w.form, composeBase, { buildPack: "railpack" });
    expect(w.buildPackValid.value).toBe(true);
    const payload = w.buildPayload();
    expect(payload.build_pack).toBe("railpack");
    expect("compose_file" in payload).toBe(false);
    expect("compose_service" in payload).toBe(false);

    w.form.buildPack = "docker_compose";
    w.form.sourceType = "dockerfile";
    const dockerfile = w.buildPayload();
    expect(dockerfile.build_pack).toBe("");
    expect("compose_file" in dockerfile).toBe(false);
    wrapper.unmount();
  });
});

describe("docker compose build pack fields", () => {
  it("shows the file and service inputs only while the pack is selected", async () => {
    const { w, wrapper } = await mountStep();
    expect(wrapper.find(".app-compose-pack-fields").exists()).toBe(false);

    w.form.buildPack = "docker_compose";
    await nextTick();
    const fields = wrapper.find(".app-compose-pack-fields");
    expect(fields.exists()).toBe(true);
    const inputs = fields.findAll("input");
    expect(inputs).toHaveLength(2);
    expect(inputs[0]!.attributes("placeholder")).toBe("docker-compose.yml");
    expect(inputs[1]!.attributes("placeholder")).toBe("web");

    await inputs[0]!.setValue("deploy/compose.yml");
    await inputs[1]!.setValue("api");
    expect(w.form.composeFile).toBe("deploy/compose.yml");
    expect(w.form.composeService).toBe("api");

    w.form.buildPack = "railpack";
    await nextTick();
    expect(wrapper.find(".app-compose-pack-fields").exists()).toBe(false);
    wrapper.unmount();
  });

  it("resolves the fields in Vietnamese without raw keys", async () => {
    const { w, wrapper } = await mountStep();
    w.form.buildPack = "docker_compose";
    syncComposerLocale("vi");
    await nextTick();
    const text = wrapper.find(".app-compose-pack-fields").text();
    expect(text).toContain("Đường dẫn file compose");
    expect(text).toContain("Service web");
    expect(text).not.toContain("applications.wizard");
    syncComposerLocale("en");
    wrapper.unmount();
  });
});
