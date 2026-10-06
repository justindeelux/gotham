import { http } from "@/shared/api/http";
import { activeLocale, i18n } from "@/shared/i18n";
import { isApiError, stripErrorPrefix } from "@/features/servers";
import type { ServiceDomainRoute } from "@/features/services";
import { checkTemplateField, validateTemplateFields } from "@/features/templates/schemas/templates";

/**
 * Typed client for the template routes served by `internal/templates`
 * (see routes.go and schema.go for the contract):
 *
 *   GET  /templates
 *   GET  /templates/{slug}
 *   POST /templates/{slug}/render
 *
 * Paths are relative to the shared axios instance (`baseURL: /api/v1`).
 *
 * The render contract (see RenderResult in internal/templates/render.go):
 * secret fields are rendered as `${field}` references in `compose_yaml` and
 * their values are returned in the response's `env` map. `env` must be passed
 * to `POST /v1/services` so the services pipeline substitutes and redacts the
 * secrets; the wizard never places a secret value anywhere else.
 */

/** Input type of one form field (see FieldType in schema.go). */
export type TemplateFieldType = "text" | "secret" | "number" | "select" | "bool";

/** One form field of a template (see Field in schema.go). */
export interface TemplateField {
  key: string;
  label: string;
  type: TemplateFieldType;
  required: boolean;
  /** Value used when the caller supplies none; absent on secret fields. */
  default?: string;
  placeholder?: string;
  help?: string;
  /** Anchored server-side as `^(?:pattern)$`. */
  pattern?: string;
  max_length?: number;
  min?: number;
  max?: number;
  options?: string[];
}

/** Catalog entry: metadata only (see templateSummary in routes.go). */
export interface TemplateSummary {
  slug: string;
  name: string;
  icon: string;
  description: string;
}

/** One template with its form schema (see templateDetail in routes.go). */
export interface TemplateDetail extends TemplateSummary {
  fields: TemplateField[];
}

/** One resolved mount of a rendered document. */
export interface TemplateMount {
  service: string;
  source?: string;
  target: string;
  read_only?: boolean;
  named?: boolean;
}

/** Parsed view of a rendered document (see specResponse in routes.go). */
export interface TemplateSpec {
  services: string[];
  domains: ServiceDomainRoute[];
  named_volumes: string[];
  mounts: TemplateMount[];
}

/** Result of POST /templates/{slug}/render. */
export interface TemplateRender {
  slug: string;
  compose_yaml: string;
  /** Values of the template's secret fields, keyed by field key. */
  env: Record<string, string>;
  spec: TemplateSpec;
}

/**
 * Form values keyed by field key, in each field's canonical string form (the
 * services pipeline accepts a string for every field type). Keeping one value
 * type keeps the dynamic form, the preview and the render body identical.
 */
export type TemplateValues = Record<string, string>;

/** Wire envelope for the catalog. */
interface TemplateListEnvelope {
  templates: TemplateSummary[];
}

/** Wire envelope for one template. */
interface TemplateEnvelope {
  template: TemplateDetail;
}

/** listTemplates returns the catalog metadata, newest template first. */
export async function listTemplates(): Promise<TemplateSummary[]> {
  const response = await http.get<TemplateListEnvelope>("/templates");
  return response.data.templates ?? [];
}

/** getTemplate returns one template's metadata plus its form schema. */
export async function getTemplate(slug: string): Promise<TemplateDetail> {
  const response = await http.get<TemplateEnvelope>(`/templates/${slug}`);
  return response.data.template;
}

/**
 * renderTemplate validates values against the template's form and returns the
 * rendered compose document, the secret `env` map and the parsed spec. An
 * invalid value answers 400 with the field message.
 */
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

/**
 * templateValuesFromFields seeds the form with each field's default, so an
 * untouched optional field submits the value the server would have used
 * anyway. Secret fields never carry a default (see parseField) and start
 * empty.
 */
export function templateValuesFromFields(fields: TemplateField[]): TemplateValues {
  const values: TemplateValues = {};
  for (const field of fields) {
    values[field.key] = field.default ?? "";
  }
  return values;
}

/**
 * checkTemplateValue mirrors `Field.check` in internal/templates/render.go so
 * the form blocks an invalid value before the render request is sent. Returns
 * null when the value is acceptable, otherwise the message to display.
 * Schema-backed: see `schemas/templates.ts templateFieldSchema`.
 */
export function checkTemplateValue(
  field: TemplateField,
  value: string,
): string | null {
  return checkTemplateField(field, value);
}

/** validateTemplateValues returns the first message of every invalid field. */
export function validateTemplateValues(
  fields: TemplateField[],
  values: TemplateValues,
): Record<string, string> {
  return validateTemplateFields(fields, values);
}

/**
 * buildTemplateRenderValues drops empty values so an optional field falls back
 * to its declared default server-side. Required fields are blocked by
 * {@link validateTemplateValues} before this is reached.
 */
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

/** describeTemplateError maps a thrown error to a user-facing message.
 *
 * Classification still runs on the raw error (status plus the stripped
 * server message, exactly as before); only the curated fallback summaries
 * resolve in the active locale. Actionable server text passes through
 * untouched so secrets stay redacted and diagnostics stay intact.
 */
export function describeTemplateError(error: unknown): string {
  // Tracks the locale when called during render or inside a computed, so
  // retained failures refresh on a language switch.
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
      return (
      stripErrorPrefix(error.message) ||
      text("templates.errors.invalidValues")
    );
    }
    if (error.status === 404) {
      return (
        stripErrorPrefix(error.message) ||
        text("templates.errors.notFound")
      );
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

/**
 * templateOverlayDescription renders a curated catalog description for a
 * bundled template slug, falling back to the provider metadata for unknown
 * or operator-provided templates. The slug itself is never translated.
 */
export function templateOverlayDescription(template: TemplateSummary): string {
  // Tracks the locale when called during render or inside a computed.
  void activeLocale.value;
  const key = `templates.overlay.${template.slug}.description`;
  if (i18n.global.te(key)) {
    return String(i18n.global.t(key));
  }
  return template.description;
}

/**
 * templateOverlayFieldHelp renders curated catalog help for one bundled
 * template field, falling back to the provider help for unknown templates,
 * unknown fields, or fields without a curated entry. Keys, labels,
 * placeholders and secret values always stay provider data.
 */
export function templateOverlayFieldHelp(
  slug: string,
  field: TemplateField,
): string | undefined {
  // Tracks the locale when called during render or inside a computed.
  void activeLocale.value;
  const key = `templates.overlay.${slug}.fields.${field.key}.help`;
  if (i18n.global.te(key)) {
    return String(i18n.global.t(key));
  }
  return field.help;
}
