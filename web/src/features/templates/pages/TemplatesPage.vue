<script setup lang="ts">
import { NButton } from "naive-ui";
import { computed, onMounted, ref } from "vue";
import { useRoute } from "vue-router";

import TemplateGallery from "@/features/templates/components/TemplateGallery.vue";
import TemplateWizard from "@/features/templates/components/TemplateWizard.vue";
import { useTemplatesStore } from "@/features/templates/stores/templates";
import { activeLocale, i18n } from "@/shared/i18n";

/**
 * Template library page: the gallery plus the deploy wizard, backed by
 * `GET /api/v1/templates`. The same components back the gallery tab on the
 * services page.
 *
 * Scope lands through the query when the user arrives from an environment
 * page (`?projectId=&environmentId=`), so the wizard takes the same project
 * and environment; otherwise the wizard's scope picker asks (brief 5).
 */

const templatesStore = useTemplatesStore();
const route = useRoute();

/**
 * t renders page copy in the active locale (tracks language switches).
 * Called during render, so labels refresh without closing the wizard.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

const wizardOpen = ref(false);
const wizardSlug = ref("");

/** queryText reads one string query value (the env page links it). */
function queryText(value: unknown): string {
  return typeof value === "string" ? value : "";
}

/** scopeProjectId preselects the wizard scope from the env-page link. */
const scopeProjectId = computed<string>(() => queryText(route.query.projectId));

/** scopeEnvironmentId preselects the wizard scope from the env-page link. */
const scopeEnvironmentId = computed<string>(() => queryText(route.query.environmentId));

/** templatePlaceholder is the engine's only supported placeholder form. */
const templatePlaceholder = "{{ .field }}";

/** openWizard opens the wizard for one gallery card. */
function openWizard(slug: string): void {
  wizardSlug.value = slug;
  wizardOpen.value = true;
}

onMounted(() => {
  void templatesStore.fetchTemplates().catch(() => undefined);
});
</script>

<template>
  <div class="templates-page">
    <div class="page-head">
      <p class="eyebrow">{{ t("templates.page.eyebrow") }}</p>
      <h1>{{ t("templates.page.title") }}</h1>
      <p class="page-desc">
        {{ t("templates.page.description") }}
      </p>
    </div>

    <NButton
      v-if="templatesStore.error"
      size="small"
      @click="templatesStore.fetchTemplates().catch(() => undefined)"
    >
      {{ t("common.actions.retry") }}
    </NButton>

    <TemplateGallery
      :templates="templatesStore.templates"
      :loading="templatesStore.loading"
      :error="templatesStore.error"
      @select="openWizard"
    />

    <div class="callout">
      <h4>{{ t("templates.page.calloutTitle") }}</h4>
      <p>
        {{ t("templates.page.calloutYaml", { file: "templates/{slug}/template.yaml" }) }}
      </p>
      <p>
        {{
          t("templates.page.calloutComposeUses", {
            file: "templates/{slug}/compose.yaml",
            placeholder: templatePlaceholder,
          })
        }}
        {{ t("templates.page.calloutComposeNote") }}
      </p>
    </div>

    <TemplateWizard
      v-model:show="wizardOpen"
      :slug="wizardSlug"
      :project-id="scopeProjectId"
      :environment-id="scopeEnvironmentId"
    />
  </div>
</template>

<style scoped>
.templates-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.page-head {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
  margin: 0;
}

.page-head h1 {
  font-size: var(--text-2xl);
  line-height: 1.25;
  color: var(--fg-2);
  margin: 0;
}

.page-desc {
  color: var(--muted);
  margin: 0;
  max-width: 72ch;
}

.callout {
  border: 1px solid var(--border);
  border-left: 4px solid var(--accent);
  border-radius: var(--radius-sm);
  background: var(--surface);
  padding: var(--space-3) var(--space-4);
}

.callout h4 {
  font-size: var(--text-sm);
  margin: 0 0 var(--space-2);
}

.callout p {
  font-size: var(--text-xs);
  color: var(--muted);
  margin: 0 0 var(--space-2);
}

.callout p:last-child {
  margin-bottom: 0;
}

.mono {
  font-family: var(--font-mono);
}
</style>
