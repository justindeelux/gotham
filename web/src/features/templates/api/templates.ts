import { http } from "@/shared/api/http";
import { isApiError, stripErrorPrefix } from "@/features/servers";
import type { ServiceDomainRoute } from "@/features/services";

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
 */
export function checkTemplateValue(
  field: TemplateField,
  value: string,
): string | null {
  switch (field.type) {
    case "text":
    case "secret": {
      if (field.required && value.trim() === "") {
        return "This field is required.";
      }
      if (field.max_length !== undefined && [...value].length > field.max_length) {
        return `Must be at most ${field.max_length} characters.`;
      }
      if (field.pattern !== undefined && !matchesPattern(field.pattern, value)) {
        return "Does not match the required format.";
      }
      return null;
    }
    case "number": {
      const trimmed = value.trim();
      if (trimmed === "") {
        return field.required ? "This field is required." : null;
      }
      if (!/^-?\d+$/.test(trimmed)) {
        return "Must be a whole number.";
      }
      const parsed = Number(trimmed);
      if (field.min !== undefined && parsed < field.min) {
        return `Must be at least ${field.min}.`;
      }
      if (field.max !== undefined && parsed > field.max) {
        return `Must be at most ${field.max}.`;
      }
      return null;
    }
    case "bool":
      return value === "true" || value === "false"
        ? null
        : "Must be true or false.";
    case "select": {
      if ((field.options ?? []).includes(value.trim())) {
        return null;
      }
      if (field.required && value.trim() === "") {
        return "This field is required.";
      }
      return `Must be one of: ${(field.options ?? []).join(", ")}.`;
    }
    default:
      return null;
  }
}

/** matchesPattern anchors the field pattern exactly like the server does. */
function matchesPattern(pattern: string, value: string): boolean {
  try {
    return new RegExp(`^(?:${pattern})$`).test(value);
  } catch {
    // The server rejects an uncompilable pattern at catalog load, so this can
    // only mean the pattern arrived through a newer API; do not block on it.
    return true;
  }
}

/** validateTemplateValues returns the first message of every invalid field. */
export function validateTemplateValues(
  fields: TemplateField[],
  values: TemplateValues,
): Record<string, string> {
  const errors: Record<string, string> = {};
  for (const field of fields) {
    const message = checkTemplateValue(field, values[field.key] ?? "");
    if (message !== null) {
      errors[field.key] = message;
    }
  }
  return errors;
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

/** describeTemplateError maps a thrown error to a user-facing message. */
export function describeTemplateError(error: unknown): string {
  if (isApiError(error)) {
    if (error.status === 401) {
      return "Your session expired. Please sign in again.";
    }
    if (error.status === 400) {
      return (
      stripErrorPrefix(error.message) ||
      "Invalid template values. Check the highlighted fields."
    );
    }
    if (error.status === 404) {
      return "Template not found. The catalog may have changed — reload the page.";
    }
    if (error.status === 503) {
      return "Services are disabled on the control plane (FEATURE_SERVICES=false).";
    }
    return stripErrorPrefix(error.message) || "Request failed";
  }
  if (error instanceof Error) {
    return stripErrorPrefix(error.message) || "Something went wrong. Please try again.";
  }
  return "Something went wrong. Please try again.";
}
