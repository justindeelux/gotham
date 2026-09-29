import { http } from "./http";
import { isApiError } from "./servers";

/**
 * Typed client for the preview-binding route served by `internal/webhooks`
 * (see routes.go for the contract):
 *
 *   GET /applications/{id}/previews
 *
 * The route exists only while FEATURE_PREVIEWS is on; disabled, it answers 404
 * and the previews surface is hidden rather than rendered as an error. Paths
 * are relative to the shared axios instance (`baseURL: /api/v1`), so the auth
 * header and refresh-on-401 behaviour come from `./http` unchanged.
 */

/**
 * Lifecycle of one preview binding (see the Preview* constants in
 * internal/webhooks/previews.go): `active` is a provisioned sibling,
 * `deploying` a reserved binding whose revision is being queued, `closing` a
 * teardown in flight, `failed` reserved for the previews screen contract and
 * `deleted` a closed pull request whose audit row was kept.
 */
export type PreviewState =
  | "active"
  | "deploying"
  | "closing"
  | "failed"
  | "deleted";

/** One preview binding as `previewResponse` in routes.go returns it. */
export interface Preview {
  id: string;
  application_id: string;
  /** The sibling application; the nil UUID when the binding has none. */
  preview_application_id?: string;
  provider: string;
  repo: string;
  pr_number: number;
  branch: string;
  head_sha: string;
  host: string;
  state: PreviewState;
  created_at: string;
  updated_at: string;
  deleted_at?: string;
}

/** Wire envelope for a preview list. */
interface PreviewListEnvelope {
  previews: Preview[];
}

/** listPreviews returns one application's preview bindings, newest first. */
export async function listPreviews(applicationId: string): Promise<Preview[]> {
  const response = await http.get<PreviewListEnvelope>(
    `/applications/${applicationId}/previews`,
  );
  return response.data.previews ?? [];
}

/**
 * previewURL builds the clickable preview address. Previews are HTTP-only by
 * design (see the BE-8.1 residuals: the sibling gets no certificate of its
 * own), so `http://` is what actually answers.
 */
export function previewURL(host: string): string {
  return `http://${host}`;
}

/** previewStateTagType maps a preview state onto a tag style. */
export function previewStateTagType(
  state: PreviewState,
): "success" | "warning" | "error" | "default" {
  switch (state) {
    case "active":
      return "success";
    case "deploying":
    case "closing":
      return "warning";
    case "failed":
      return "error";
    default:
      return "default";
  }
}

/** isFeatureDisabled reports whether an error is a feature-flag 404. */
export function isFeatureDisabled(error: unknown): boolean {
  return isApiError(error) && error.status === 404;
}

/** describePreviewError maps a thrown error to a user-facing message. */
export function describePreviewError(error: unknown): string {
  if (isApiError(error)) {
    if (error.status === 401) {
      return "Your session expired. Please sign in again.";
    }
    if (error.status === 404) {
      return "Preview deployments are not enabled on this control plane (FEATURE_PREVIEWS=false).";
    }
    return error.message || "Request failed";
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "Something went wrong. Please try again.";
}
