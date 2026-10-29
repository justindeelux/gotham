import { http } from "@/shared/api/http";
import { activeLocale, i18n } from "@/shared/i18n";
import { isApiError, stripErrorPrefix } from "@/features/servers";
import type { ServiceDomainRoute } from "@/features/services";
import { checkTemplateField, validateTemplateFields } from "@/features/templates/schemas/templates";

export type TemplateFieldType = "text" | "secret" | "number" | "select" | "bool";

export interface TemplateField {
  key: string;
  label: string;
  type: TemplateFieldType;
  required: boolean;
  default?: string;
  placeholder?: string;
  help?: string;
  pattern?: string;
  max_length?: number;
  min?: number;
  max?: number;
  options?: string[];
  generate?: boolean;
}

export interface TemplateSummary {
  slug: string;
  name: string;
  icon: string;
  description: string;
}

export interface TemplateDetail extends TemplateSummary {
  fields: TemplateField[];
}

export interface TemplateMount {
  service: string;
  source?: string;
  target: string;
  read_only?: boolean;
  named?: boolean;
}

export interface TemplateSpec {
  services: string[];
  domains: ServiceDomainRoute[];
  named_volumes: string[];
  mounts: TemplateMount[];
}

export interface TemplateRender {
  slug: string;
  compose_yaml: string;
  env: Record<string, string>;
  spec: TemplateSpec;
}

export type TemplateValues = Record<string, string>;

interface TemplateListEnvelope {
  templates: TemplateSummary[];
}

interface TemplateEnvelope {
  template: TemplateDetail;
}

export async function listTemplates(): Promise<TemplateSummary[]> {
  const response = await http.get<TemplateListEnvelope>("/templates");
  return response.data.templates ?? [];
}

export async function getTemplate(slug: string): Promise<TemplateDetail> {
  const response = await http.get<TemplateEnvelope>(`/templates/${slug}`);
  return response.data.template;
}

export async function renderTemplate(
  slug: string,
  values: TemplateValues,
): Promise<TemplateRender> {
  const response = await http.post<TemplateRender>(
    `/templates/${slug}/render`,
    { values },
  );
  return response.data;
}

export function templateValuesFromFields(fields: TemplateField[]): TemplateValues {
  const values: TemplateValues = {};
  for (const field of fields) {
    values[field.key] = field.default ?? "";
  }
  return values;
}

export function checkTemplateValue(
  field: TemplateField,
  value: string,
): string | null {
  return checkTemplateField(field, value);
}

export function validateTemplateValues(
  fields: TemplateField[],
  values: TemplateValues,
): Record<string, string> {
  return validateTemplateFields(fields, values);
}

export function buildTemplateRenderValues(
  values: TemplateValues,
): TemplateValues {
  const payload: TemplateValues = {};
  for (const [key, value] of Object.entries(values)) {
    if (value !== "") {
      payload[key] = value;
    }
  }
  return payload;
}

export function describeTemplateError(error: unknown): string {
  void activeLocale.value;
  const text = (
    key: string,
    params?: Record<string, string | number>,
  ): string => String(i18n.global.t(key, params ?? {}));
  if (isApiError(error)) {
    if (error.status === 401) {
      return text("templates.errors.sessionExpired");
    }
    if (error.status === 400) {
      const detail = stripErrorPrefix(error.message);
      return detail
        ? text("templates.errors.invalidValuesWithDetail", { detail })
        : text("templates.errors.invalidValues");
    }
    if (error.status === 404) {
      const detail = stripErrorPrefix(error.message);
      return detail
        ? text("templates.errors.notFoundWithDetail", { detail })
        : text("templates.errors.notFound");
    }
    if (error.status === 503) {
      return text("templates.errors.disabled");
    }
    return stripErrorPrefix(error.message) || text("common.errors.requestFailed");
  }
  if (error instanceof Error) {
    return stripErrorPrefix(error.message) || text("common.errors.unexpected");
  }
  return text("common.errors.unexpected");
}

export function templateOverlayDescription(template: TemplateSummary): string {
  void activeLocale.value;
  const key = `templates.overlay.${template.slug}.description`;
  if (i18n.global.te(key)) {
    return String(i18n.global.t(key));
  }
  return template.description;
}

export function templateOverlayFieldHelp(
  slug: string,
  field: TemplateField,
): string | undefined {
  void activeLocale.value;
  const key = `templates.overlay.${slug}.fields.${field.key}.help`;
  if (i18n.global.te(key)) {
    return String(i18n.global.t(key));
  }
  return field.help;
}
