// Differential test (JUS-23): application schemas and env warnings must
// reproduce the recorded outcomes of the hand-written checks they replace.
// Recorded 2026-10-04 from the pre-migration code.
import { mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";
import { defineComponent, h, nextTick, ref } from "vue";

import {
  countDroppedEnvRows,
  hasEnvKeyWarnings,
  isRecommendedEnvKey,
} from "@/features/applications/schemas/env";
import { hostDomainSchema, isPublicGitUrl } from "@/features/applications/schemas/applications";
import { isValidDomain } from "@/features/applications/composables/useApplicationDomain";
import { useCreateAppWizard } from "@/features/applications/composables/useCreateAppWizard";
import { fieldErrors } from "@/shared/validation/naiveAdapter";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

describe("env convention warnings stay non-blocking", () => {
  it("matches recorded recommendations", () => {
    const rows: Array<{ key: string; recommended: boolean }> = [
      { key: "", recommended: false },
      { key: "   ", recommended: false },
      { key: "NODE_ENV", recommended: true },
      { key: "node_env", recommended: false },
      { key: "A", recommended: true },
      { key: "A1_", recommended: true },
      { key: "1A", recommended: false },
      { key: "A-B", recommended: false },
      { key: "A B", recommended: false },
      { key: "A=B", recommended: false },
      { key: "FOOé", recommended: false },
    ];
    for (const row of rows) {
      expect(isRecommendedEnvKey(row.key)).toBe(row.recommended);
    }
  });

  it("matches recorded warning/dropped counts", () => {
    const sets: Array<{ rows: Array<{ key: string; value: string }>; warn: boolean; dropped: number }> = [
      { rows: [], warn: false, dropped: 0 },
      { rows: [{"key": "", "value": ""}], warn: false, dropped: 0 },
      { rows: [{"key": "", "value": "x"}], warn: false, dropped: 1 },
      { rows: [{"key": "ok_key", "value": "v"}], warn: true, dropped: 0 },
      { rows: [{"key": "lower", "value": "v"}], warn: true, dropped: 0 },
      { rows: [{"key": "GOOD", "value": "v"}, {"key": "", "value": "dropped"}, {"key": "bad-key", "value": "v"}], warn: true, dropped: 1 },
    ];
    for (const set of sets) {
      expect(hasEnvKeyWarnings(set.rows)).toBe(set.warn);
      expect(countDroppedEnvRows(set.rows)).toBe(set.dropped);
    }
  });
});

describe("domain schema matches recorded outcomes", () => {
  it("matches recorded validity and keeps the exact message", () => {
    const rows: Array<{ value: string; valid: boolean }> = [
      { value: "", valid: true },
      { value: "app.example.com", valid: true },
      { value: "UPPER.example", valid: false },
      { value: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", valid: false },
      { value: "-bad-", valid: false },
      { value: "ok-host_1", valid: false },
      { value: "x.y", valid: true },
      { value: "a..b", valid: false },
    ];
    for (const row of rows) {
      expect(isValidDomain(row.value)).toBe(row.valid);
      expect(hostDomainSchema.safeParse(row.value).success).toBe(row.valid);
    }
    expect(fieldErrors(hostDomainSchema, "UPPER.example")[0]).toBe(
      "Enter a plain hostname such as app.example.com (letters, digits, hyphens and dots; no wildcard).",
    );
    setLocale("vi", null);
    expect(fieldErrors(hostDomainSchema, "UPPER.example")[0]).toBe(
      "Nhập hostname thuần như app.example.com (chữ cái, chữ số, gạch ngang và dấu chấm; không dùng ký tự đại diện).",
    );
    setLocale("en", null);
  });
});

describe("wizard gates match recorded outcomes", () => {
  it("sourceValid and runtimeValid agree row by row", () => {
    setActivePinia(createPinia());
    let wiz: ReturnType<typeof useCreateAppWizard> | null = null;
    const Harness = defineComponent({
      setup() {
        wiz = useCreateAppWizard(ref(false), (() => undefined) as never);
        return () => h("div");
      },
    });
    const wrapper = mount({ render: () => h(NMessageProvider, null, { default: () => h(Harness) }) });
    const w = wiz!;
    const reset = {
      sourceType: "git_public", providerId: "", publicCloneUrl: "", repoFullName: "", cloneUrl: "", branch: "main", name: "",
      buildPack: "", serverId: "", port: 3000, hostPort: null, baseDomain: "", env: [], storage: [],
    };
    const sourceCases: Array<{ name: string; patch: Record<string, unknown>; sourceValid: boolean }> = [
      { name: "empty", patch: {}, sourceValid: false },
      { name: "public-valid", patch: {"sourceType": "git_public", "publicCloneUrl": "https://github.com/o/r.git", "branch": "main", "name": "storefront"}, sourceValid: true },
      { name: "public-git-scheme", patch: {"sourceType": "git_public", "publicCloneUrl": "git://git.internal/o/r.git", "branch": "main", "name": "storefront"}, sourceValid: true },
      { name: "public-token-url", patch: {"sourceType": "git_public", "publicCloneUrl": "https://user:tok@github.com/o/r.git", "branch": "main", "name": "storefront"}, sourceValid: false },
      { name: "public-no-host", patch: {"sourceType": "git_public", "publicCloneUrl": "https:///o/r.git", "branch": "main", "name": "storefront"}, sourceValid: false },
      { name: "public-blank-url", patch: {"sourceType": "git_public", "publicCloneUrl": "   ", "branch": "main", "name": "storefront"}, sourceValid: false },
      { name: "public-ssh-url", patch: {"sourceType": "git_public", "publicCloneUrl": "ssh://git@h/o/r.git", "branch": "main", "name": "storefront"}, sourceValid: false },
      { name: "public-scp-url", patch: {"sourceType": "git_public", "publicCloneUrl": "git@h:o/r.git", "branch": "main", "name": "storefront"}, sourceValid: false },
      { name: "public-bad-name", patch: {"sourceType": "git_public", "publicCloneUrl": "https://github.com/o/r.git", "branch": "main", "name": "Bad_Name!"}, sourceValid: false },
      { name: "public-short-name", patch: {"sourceType": "git_public", "publicCloneUrl": "https://github.com/o/r.git", "branch": "main", "name": "ab"}, sourceValid: false },
      { name: "public-blank-branch", patch: {"sourceType": "git_public", "publicCloneUrl": "https://github.com/o/r.git", "branch": "  ", "name": "storefront"}, sourceValid: true },
      { name: "github-blank-branch", patch: {"sourceType": "github_app", "providerId": "p1", "repoFullName": "o/r", "cloneUrl": "git@h:o/r.git", "branch": "  ", "name": "abc"}, sourceValid: false },
      { name: "private-soon", patch: {"sourceType": "git_private", "branch": "main", "name": "abc"}, sourceValid: false },
      { name: "private-soon-with-url", patch: {"sourceType": "git_private", "publicCloneUrl": "git@h:o/r.git", "branch": "main", "name": "abc"}, sourceValid: false },
      { name: "github-valid", patch: {"sourceType": "github_app", "providerId": "p1", "repoFullName": "o/r", "cloneUrl": "git@h:o/r.git", "branch": "main", "name": "abc"}, sourceValid: true },
      { name: "github-no-clone", patch: {"sourceType": "github_app", "providerId": "p1", "repoFullName": "o/r", "cloneUrl": "  ", "branch": "main", "name": "abc"}, sourceValid: false },
      { name: "github-no-repo", patch: {"sourceType": "github_app", "providerId": "p1", "repoFullName": "", "cloneUrl": "git@h:o/r.git", "branch": "main", "name": "abc"}, sourceValid: false },
      { name: "gitlab-valid", patch: {"sourceType": "gitlab_app", "providerId": "p1", "repoFullName": "o/r", "cloneUrl": "git@h:o/r.git", "branch": "main", "name": "abc"}, sourceValid: true },
      { name: "dockerfile-placeholder", patch: {"sourceType": "dockerfile", "branch": "main", "name": "abc"}, sourceValid: false },
      { name: "compose-placeholder", patch: {"sourceType": "compose", "branch": "main", "name": "abc"}, sourceValid: false },
      { name: "image-placeholder", patch: {"sourceType": "image", "branch": "main", "name": "abc"}, sourceValid: false },
      { name: "name-31-chars", patch: {"sourceType": "git_public", "publicCloneUrl": "https://github.com/o/r.git", "branch": "main", "name": "abbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, sourceValid: true },
      { name: "name-32-chars", patch: {"sourceType": "git_public", "publicCloneUrl": "https://github.com/o/r.git", "branch": "main", "name": "abbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, sourceValid: false },
      { name: "no-provider-else-valid", patch: {"sourceType":"github_app","providerId":"","repoFullName":"o/r","cloneUrl":"git@h:o/r.git","branch":"main","name":"storefront"}, sourceValid: false },
      { name: "no-repo-else-valid", patch: {"sourceType":"github_app","providerId":"p1","repoFullName":"","cloneUrl":"git@h:o/r.git","branch":"main","name":"storefront"}, sourceValid: false },
    ];
    for (const c of sourceCases) {
      Object.assign(w.form, { ...reset }, c.patch);
      expect({ name: c.name, actual: w.sourceValid.value }).toEqual({ name: c.name, actual: c.sourceValid });
    }
    const runtimeCases: Array<{ name: string; patch: Record<string, unknown>; runtimeValid: boolean }> = [
      { name: "empty", patch: {}, runtimeValid: false },
      { name: "valid", patch: {"serverId": "s1", "port": 3000, "hostPort": null, "baseDomain": ""}, runtimeValid: true },
      { name: "null-port", patch: {"serverId": "s1", "port": null, "hostPort": null, "baseDomain": ""}, runtimeValid: false },
      { name: "port-0", patch: {"serverId": "s1", "port": 0, "hostPort": null, "baseDomain": ""}, runtimeValid: false },
      { name: "port-65536", patch: {"serverId": "s1", "port": 65536, "hostPort": null, "baseDomain": ""}, runtimeValid: false },
      { name: "port-1.5", patch: {"serverId": "s1", "port": 1.5, "hostPort": null, "baseDomain": ""}, runtimeValid: false },
      { name: "port-nan", patch: { serverId: "s1", port: NaN, hostPort: null, baseDomain: "" }, runtimeValid: false },
      { name: "hostport-0", patch: {"serverId": "s1", "port": 3000, "hostPort": 0, "baseDomain": ""}, runtimeValid: true },
      { name: "hostport-neg", patch: {"serverId": "s1", "port": 3000, "hostPort": -1, "baseDomain": ""}, runtimeValid: false },
      { name: "hostport-big", patch: {"serverId": "s1", "port": 3000, "hostPort": 70000, "baseDomain": ""}, runtimeValid: false },
      { name: "domain-ok", patch: {"serverId": "s1", "port": 3000, "hostPort": null, "baseDomain": "app.gotham.dev"}, runtimeValid: true },
      { name: "domain-bad", patch: {"serverId": "s1", "port": 3000, "hostPort": null, "baseDomain": "not a domain"}, runtimeValid: false },
      { name: "domain-upper", patch: {"serverId": "s1", "port": 3000, "hostPort": null, "baseDomain": "APP.EXAMPLE.COM"}, runtimeValid: false },
      { name: "domain-spaces", patch: {"serverId": "s1", "port": 3000, "hostPort": null, "baseDomain": "  app.gotham.dev  "}, runtimeValid: true },
      { name: "no-server-else-valid", patch: {"serverId":"","port":3000,"hostPort":null,"baseDomain":""}, runtimeValid: false },
    ];
    for (const c of runtimeCases) {
      Object.assign(w.form, { serverId: "", port: 3000, hostPort: null, baseDomain: "" }, c.patch);
      expect({ name: c.name, actual: w.runtimeValid.value }).toEqual({ name: c.name, actual: c.runtimeValid });
    }
    wrapper.unmount();
  });
});

describe("wizard create payload carries the source type", () => {
  it("maps each source type to its provider/repo/clone_url shape", () => {
    setActivePinia(createPinia());
    let wiz: ReturnType<typeof useCreateAppWizard> | null = null;
    const Harness = defineComponent({
      setup() {
        wiz = useCreateAppWizard(ref(false), (() => undefined) as never);
        return () => h("div");
      },
    });
    const wrapper = mount({ render: () => h(NMessageProvider, null, { default: () => h(Harness) }) });
    const w = wiz!;
    w.providersStore.providers = [
      { id: "p1", provider: "github", base_url: "", connected: true, scopes: "", created_at: "", updated_at: "" },
      { id: "p2", provider: "gitlab", base_url: "", connected: true, scopes: "", created_at: "", updated_at: "" },
    ];

    // Provider options follow the source type: github_app lists GitHub App
    // connections (GS-5), gitlab_app keeps the OAuth provider list.
    w.githubAppStore.apps = [
      { id: "g1", app_id: 1, slug: "gotham", name: "gotham", base_url: "", connected: true, installations: [] },
    ];
    Object.assign(w.form, { sourceType: "github_app" });
    expect(w.providerOptions.value.map((item) => item.value)).toEqual(["g1"]);
    Object.assign(w.form, { sourceType: "gitlab_app" });
    expect(w.providerOptions.value.map((item) => item.value)).toEqual(["p2"]);

    // git_public carries no provider.
    Object.assign(w.form, {
      sourceType: "git_public", providerId: "", publicCloneUrl: "https://github.com/o/r.git",
      repoFullName: "", cloneUrl: "", branch: "main", name: "storefront",
    });
    expect(w.buildPayload()).toMatchObject({
      provider: "",
      repo: "https://github.com/o/r.git",
      clone_url: "https://github.com/o/r.git",
      source_type: "git_public",
    });

    // github_app carries the matching provider slug.
    Object.assign(w.form, {
      sourceType: "github_app", providerId: "p1", repoFullName: "o/r",
      cloneUrl: "git@github.com:o/r.git", branch: "main", name: "abc",
    });
    expect(w.buildPayload()).toMatchObject({
      provider: "github",
      repo: "o/r",
      clone_url: "git@github.com:o/r.git",
      source_type: "github_app",
    });
    wrapper.unmount();
  });

  it("clears the previous type's fields on source type switch", async () => {
    setActivePinia(createPinia());
    let wiz: ReturnType<typeof useCreateAppWizard> | null = null;
    const Harness = defineComponent({
      setup() {
        wiz = useCreateAppWizard(ref(false), (() => undefined) as never);
        return () => h("div");
      },
    });
    const wrapper = mount({ render: () => h(NMessageProvider, null, { default: () => h(Harness) }) });
    const w = wiz!;
    Object.assign(w.form, {
      sourceType: "github_app", providerId: "p1", repoFullName: "o/r", cloneUrl: "git@h:o/r.git",
    });
    w.form.sourceType = "gitlab_app";
    await nextTick();
    expect({ providerId: w.form.providerId, repoFullName: w.form.repoFullName, cloneUrl: w.form.cloneUrl }).toEqual({
      providerId: "",
      repoFullName: "",
      cloneUrl: "",
    });
    wrapper.unmount();
  });

  it("leaves only implemented source types selectable", () => {
    setActivePinia(createPinia());
    let wiz: ReturnType<typeof useCreateAppWizard> | null = null;
    const Harness = defineComponent({
      setup() {
        wiz = useCreateAppWizard(ref(false), (() => undefined) as never);
        return () => h("div");
      },
    });
    const wrapper = mount({ render: () => h(NMessageProvider, null, { default: () => h(Harness) }) });
    const w = wiz!;
    const options = Object.fromEntries(
      w.sourceTypeOptions.value.map((item) => [item.value, item.disabled === true]),
    );
    // git_public, the connected-provider flows and pasted Dockerfiles deploy;
    // everything else stays disabled until its package lands (GS-4, GS-8..9).
    expect(options).toEqual({
      git_public: false,
      git_private: true,
      github_app: false,
      gitlab_app: false,
      dockerfile: false,
      compose: true,
      image: true,
    });
    wrapper.unmount();
  });

  it("renders an empty public branch as the repository default", () => {
    setActivePinia(createPinia());
    let wiz: ReturnType<typeof useCreateAppWizard> | null = null;
    const Harness = defineComponent({
      setup() {
        wiz = useCreateAppWizard(ref(false), (() => undefined) as never);
        return () => h("div");
      },
    });
    const wrapper = mount({ render: () => h(NMessageProvider, null, { default: () => h(Harness) }) });
    const w = wiz!;
    Object.assign(w.form, {
      sourceType: "git_public",
      publicCloneUrl: "https://github.com/o/r.git",
      branch: "   ",
      name: "storefront",
    });
    expect(w.sourceValid.value).toBe(true);
    expect(w.reviewSource.value).toBe("https://github.com/o/r.git · (default branch)");
    expect(w.buildPayload()).toMatchObject({ branch: "" });
    wrapper.unmount();
  });
});

describe("isPublicGitUrl mirrors the backend allow-list", () => {
  it("accepts only credential-free http(s)/git URLs with a host", () => {
    const valid = [
      "https://github.com/o/r.git",
      "http://git.internal/o/r.git",
      "git://git.internal/o/r.git",
      "HTTPS://github.com/o/r.git",
      "  https://github.com/o/r.git  ",
    ];
    for (const url of valid) {
      expect({ url, actual: isPublicGitUrl(url) }).toEqual({ url, actual: true });
    }
    const invalid = [
      "",
      "   ",
      "ssh://git@h/o/r.git",
      "git@h:o/r.git",
      "deploy@h:o/r.git",
      "ftp://h/o/r.git",
      "demo",
      "--upload-pack=touch /tmp/pwn",
      "/srv/fixtures/demo",
      "file:///srv/fixtures/demo",
      "https://user:tok@github.com/o/r.git",
      "https://tok@github.com/o/r.git",
      "https://",
      "https:///o/r.git",
      "https://host/a b.git",
    ];
    for (const url of invalid) {
      expect({ url, actual: isPublicGitUrl(url) }).toEqual({ url, actual: false });
    }
  });
});
