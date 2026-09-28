import { defineStore } from "pinia";
import { ref } from "vue";

import {
  describeTemplateError,
  getTemplate,
  listTemplates,
  renderTemplate,
} from "../api/templates";
import type {
  TemplateDetail,
  TemplateRender,
  TemplateSummary,
  TemplateValues,
} from "../api/templates";

/**
 * Template catalog: the gallery list plus a per-slug detail (form schema)
 * cache. A render is an action, not state, so it is never cached: the wizard
 * re-renders whenever the values or the preview are shown.
 */
export const useTemplatesStore = defineStore("templates", () => {
  const templates = ref<TemplateSummary[]>([]);
  const details = ref<Record<string, TemplateDetail>>({});
  const loading = ref(false);
  const error = ref<string | null>(null);
  const detailLoading = ref(false);
  const detailError = ref<string | null>(null);

  /** fetchTemplates loads the gallery catalog. */
  async function fetchTemplates(): Promise<void> {
    loading.value = true;
    error.value = null;
    try {
      templates.value = await listTemplates();
    } catch (err) {
      error.value = describeTemplateError(err);
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
    detailError.value = null;
    try {
      const detail = await getTemplate(slug);
      details.value = { ...details.value, [slug]: detail };
      return detail;
    } catch (err) {
      detailError.value = describeTemplateError(err);
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

  return {
    templates,
    details,
    loading,
    error,
    detailLoading,
    detailError,
    fetchTemplates,
    detailOf,
    fetchDetail,
    render,
  };
});
