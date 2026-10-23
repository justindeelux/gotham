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

export const useTemplatesStore = defineStore("templates", () => {
  const templates = ref<TemplateSummary[]>([]);
  const details = ref<Record<string, TemplateDetail>>({});
  const loading = ref(false);
  const listFailure: Ref<unknown> = ref(null);
  const error = computed<string | null>(() => {
    if (listFailure.value === null) {
      return null;
    }
    void activeLocale.value;
    return describeTemplateError(listFailure.value);
  });
  const detailLoading = ref(false);
  const detailFailure: Ref<unknown> = ref(null);
  const detailError = computed<string | null>(() => {
    if (detailFailure.value === null) {
      return null;
    }
    void activeLocale.value;
    return describeTemplateError(detailFailure.value);
  });

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

  function detailOf(slug: string): TemplateDetail | null {
    return details.value[slug] ?? null;
  }

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

  async function render(
    slug: string,
    values: TemplateValues,
  ): Promise<TemplateRender> {
    return renderTemplate(slug, values);
  }

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
