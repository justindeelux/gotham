<script setup lang="ts">
import TemplateGallery from "@/features/templates/components/TemplateGallery.vue";
import { useTemplatesStore } from "@/features/templates";
import { useServicesPageContext } from "@/features/services/composables/useServicesList";

/** TemplateGalleryPanel renders the templates tab: gallery plus explainer. */
const templatesStore = useTemplatesStore();
const { templatePlaceholder, openWizard } = useServicesPageContext();
</script>

<template>
  <div class="section-head">
    <h2>Template library</h2>
    <span class="small muted">
      source: <span class="mono">GET /api/v1/templates</span> ·
      {{ templatesStore.templates.length }} templates
    </span>
  </div>
  <p class="page-desc section-desc">
    Pick a template, fill its form, and the control plane renders the
    compose document before deploying it through the agent. The form comes
    from the template schema, so a new template needs no UI change.
  </p>
  <TemplateGallery
    :templates="templatesStore.templates"
    :loading="templatesStore.loading"
    :error="templatesStore.error"
    @select="openWizard"
  />
  <div class="callout">
    <h4>How a template is structured</h4>
    <p>
      <span class="mono">templates/{slug}/template.yaml</span> declares the
      name, icon, description and the form fields (type, default,
      required, validation).
    </p>
    <p>
      <span class="mono">templates/{slug}/compose.yaml</span> uses
      <span class="mono">{{ templatePlaceholder }}</span> placeholders. The
      engine only substitutes strings and never executes code, so a
      template cannot run commands on a node. Editing a template does not
      affect a service that is already deployed.
    </p>
  </div>
</template>

<style scoped>
.page-desc {
  color: var(--muted);
  margin: 0;
  max-width: 72ch;
}

.section-head {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  flex-wrap: wrap;
  margin-top: var(--space-4);
}

.section-head h2 {
  margin: 0;
  font-size: var(--text-lg);
}

.section-desc {
  margin: var(--space-2) 0 var(--space-4);
}

.small {
  font-size: var(--text-xs);
}

.muted {
  color: var(--muted);
}

.mono {
  font-family: var(--font-mono);
}

.callout {
  margin-top: var(--space-5);
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
</style>
