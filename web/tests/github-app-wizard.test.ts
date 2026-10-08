// GitHub App wizard flow (GS-5, JUS-61): the github_app source lists
// repositories and branches of the installation through the github-app store,
// while gitlab_app keeps the OAuth provider list. No network: stores are
// seeded directly and only the derived state is asserted.
import { mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { describe, expect, it } from "vitest";
import { defineComponent, h, ref } from "vue";

import { useCreateAppWizard } from "@/features/applications/composables/useCreateAppWizard";

function harness() {
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

describe("github_app wizard flow", () => {
  it("lists installation repos and branches, payload keys off github", () => {
    const { w, wrapper } = harness();
    w.githubAppStore.apps = [
      { id: "g1", app_id: 7, slug: "gotham", name: "gotham", base_url: "", connected: true, installations: [] },
    ];
    Object.assign(w.form, { sourceType: "github_app", providerId: "g1" });
    expect(w.isGitHubAppFlow.value).toBe(true);
    expect(w.providerOptions.value.map((item) => item.value)).toEqual(["g1"]);

    w.githubAppStore.reposByApp = {
      g1: [
        { id: "1", name: "web", full_name: "acme/web", private: true, default_branch: "main", clone_url: "https://github.com/acme/web.git", ssh_url: "git@github.com:acme/web.git", html_url: "" },
      ],
    };
    expect(w.repoOptions.value.map((item) => item.value)).toEqual(["acme/web"]);

    w.handleRepoSelect("acme/web");
    expect(w.form.branch).toBe("main");
    // The github_app flow stores the https clone_url even for private repos:
    // the installation token is injected at clone time, and the token cloner
    // only accepts http(s).
    expect(w.form.cloneUrl).toBe("https://github.com/acme/web.git");

    w.githubAppStore.branchesByRepo = {
      "g1/acme/web": [
        { name: "main", commit: "abc", protected: true },
        { name: "dev", commit: "def", protected: false },
      ],
    };
    // The repo select (v-model) sets the full name; the branch list follows it.
    Object.assign(w.form, { repoFullName: "acme/web" });
    expect(w.branchOptions.value.map((item) => item.value)).toEqual(["main", "dev"]);

    Object.assign(w.form, { branch: "main", name: "web" });
    expect(w.sourceValid.value).toBe(true);
    expect(w.buildPayload()).toMatchObject({
      provider: "github",
      repo: "acme/web",
      source_type: "github_app",
    });
    wrapper.unmount();
  });

  it("gitlab_app still uses the OAuth provider list", () => {
    const { w, wrapper } = harness();
    w.providersStore.providers = [
      { id: "p2", provider: "gitlab", base_url: "", connected: true, scopes: "", created_at: "", updated_at: "" },
    ];
    Object.assign(w.form, { sourceType: "gitlab_app" });
    expect(w.isGitHubAppFlow.value).toBe(false);
    expect(w.providerOptions.value.map((item) => item.value)).toEqual(["p2"]);
    expect(w.branchOptions.value).toEqual([]);
    wrapper.unmount();
  });
});
