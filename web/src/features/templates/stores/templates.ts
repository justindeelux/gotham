import { defineStore } from "pinia";
import { computed, ref } from "vue";
import type { Ref } from "vue";

import { activeLocale } from "@/shared/i18n";

import {
  describeTemplateError,
  getTemplate,
  listTemplates,
  renderTemplate,
} from "@/features/templates/api/templates";
import type {
  TemplateDetail,
  TemplateRender,
  TemplateSummary,
  TemplateValues,
} from "@/features/templates/api/templates";

/**
 * Template catalog: the gallery list plus a per-slug detail (form schema)
 * cache. A render is an action, not state, so it is never cached: the wizard
 * re-renders whenever the values or the preview are shown.
 */
export const useTemplatesStore = defineStore("templates", () => {
  const templates = ref<TemplateSummary[]>([]);
  const details = ref<Record<string, TemplateDetail>>({});
  const loading = ref(false);
  /** listFailure retains the raw catalog refusal; error derives its display. */
  const listFailure: Ref<unknown> = ref(null);
  const error = computed<string | null>(() => {
    if (listFailure.value === null) {
      return null;
    }
    // Tracks the locale when called during render or inside a computed.
    void activeLocale.value;
    return describeTemplateError(listFailure.value);
  });
  const detailLoading = ref(false);
  /** detailFailure retains the raw detail refusal; detailError derives it. */
  const detailFailure: Ref<unknown> = ref(null);
  const detailError = computed<string | null>(() => {
    if (detailFailure.value === null) {
      return null;
    }
    // Tracks the locale when called during render or inside a computed.
    void activeLocale.value;
    return describeTemplateError(detailFailure.value);
  });

  /** fetchTemplates loads the gallery catalog. */
  async function fetchTemplates(): Promise<void> {
    loading.value = true;
    listFailure.value = null;
    try {
      templates.value = await listTemplates();
    } catch (err) {
      listFailure.value = err;
      throw err;
    } finally {
      loading.value = false;
    }
  }

  /** detailOf returns a template's cached schema, if loaded. */
  function detailOf(slug: string): TemplateDetail | null {
    return details.value[slug] ?? null;
  }

  /** fetchDetail loads (or reuses) one template's form schema. */
  async function fetchDetail(slug: string, force = false): Promise<TemplateDetail> {
    const cached = details.value[slug];
    if (cached && !force) {
      return cached;
    }
    detailLoading.value = true;
    detailFailure.value = null;
    try {
      const detail = await getTemplate(slug);
      details.value = { ...details.value, [slug]: detail };
      return detail;
    } catch (err) {
      detailFailure.value = err;
      throw err;
    } finally {
      detailLoading.value = false;
    }
  }

  /** render validates values and returns the rendered document with its env. */
  async function render(
    slug: string,
    values: TemplateValues,
  ): Promise<TemplateRender> {
    return renderTemplate(slug, values);
  }

  /**
   * reset drops the cached catalog, so the next sign-in starts empty.
   * Called on sign-out (see the auth store).
   */
  function reset(): void {
    templates.value = [];
    details.value = {};
    loading.value = false;
    listFailure.value = null;
    detailLoading.value = false;
    detailFailure.value = null;
  }

  return {
    templates,
    details,
    loading,
    error,
    detailLoading,
    detailError,
    reset,
    fetchTemplates,
    detailOf,
    fetchDetail,
    render,
  };
});
