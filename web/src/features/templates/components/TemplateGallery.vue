<script setup lang="ts">
import { NAlert, NEmpty, NSpin } from "naive-ui";

import type { TemplateSummary } from "@/features/templates/api/templates";
import { templateOverlayDescription } from "@/features/templates/api/templates";
import { cardMark } from "@/features/templates/utils/templateMark";
import { activeLocale, i18n } from "@/shared/i18n";

/**
 * Template gallery: cards from `GET /api/v1/templates`.
 *
 * The catalog carries metadata only (slug, name, icon, description), so the
 * cards show exactly that — no invented service, volume or field counts.
 */

interface Props {
  templates: TemplateSummary[];
  loading?: boolean;
  error?: string | null;
}

withDefaults(defineProps<Props>(), {
  loading: false,
  error: null,
});

const emit = defineEmits<{
  select: [slug: string];
}>();

/**
 * t renders gallery copy in the active locale (tracks language switches).
 * Card descriptions resolve through the curated overlay with fallback to
 * the provider metadata, so unknown templates keep their source copy.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

</script>

<template>
  <div class="template-gallery">
    <NAlert v-if="error" type="error" :show-icon="true">{{ error }}</NAlert>

    <NSpin :show="loading">
      <div v-if="templates.length > 0" class="template-gallery__grid">
        <button
          v-for="template in templates"
          :key="template.slug"
          type="button"
          class="tpl-card"
          :data-template="template.slug"
          :data-icon="template.icon"
          @click="emit('select', template.slug)"
        >
          <span class="tpl-mark" aria-hidden="true">{{ cardMark(template.name) }}</span>
          <span class="tpl-name">{{ template.name }}</span>
          <span class="tpl-desc">{{ templateOverlayDescription(template) }}</span>
        </button>
      </div>
      <NEmpty
        v-else-if="!loading"
        :description="t('templates.gallery.empty')"
      />
    </NSpin>
  </div>
</template>

<style scoped>
.template-gallery {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.template-gallery__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: var(--space-4);
}

.tpl-card {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  text-align: left;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-4);
  cursor: pointer;
  color: inherit;
  font: inherit;
  transition:
    border-color var(--motion-base) var(--ease-standard),
    background var(--motion-base) var(--ease-standard);
}

.tpl-card:hover {
  border-color: var(--accent);
  background: color-mix(in oklab, var(--surface) 88%, var(--accent));
}

.tpl-card:focus-visible {
  outline: none;
  box-shadow: var(--focus-ring);
}

.tpl-mark {
  width: 40px;
  height: 40px;
  border-radius: var(--radius-md);
  display: grid;
  place-items: center;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  font-family: var(--font-display);
  font-weight: 700;
  color: var(--fg-2);
}

.tpl-name {
  font-size: var(--text-base);
  font-weight: 600;
  color: var(--fg-2);
}

.tpl-desc {
  font-size: var(--text-xs);
  color: var(--muted);
}
</style>
