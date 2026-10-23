import { http } from "@/shared/api/http";
import { i18n } from "@/shared/i18n";
import { isApiError, stripErrorPrefix } from "@/features/servers";

export type PreviewState =
  | "active"
  | "deploying"
  | "closing"
  | "failed"
  | "deleted";

export interface Preview {
  id: string;
  application_id: string;
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

interface PreviewListEnvelope {
  previews: Preview[];
}

export async function listPreviews(applicationId: string): Promise<Preview[]> {
  const response = await http.get<PreviewListEnvelope>(
    `/applications/${applicationId}/previews`,
  );
  return response.data.previews ?? [];
}

export function previewURL(host: string): string {
  return `http://${host}`;
}

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

export function isFeatureDisabled(error: unknown): boolean {
  return isApiError(error) && error.status === 404;
}

export function describePreviewError(error: unknown): string {
  const t = i18n.global.t.bind(i18n.global);
  if (isApiError(error)) {
    if (error.status === 401) {
      return String(t("applications.errors.sessionExpired"));
    }
    if (error.status === 404) {
      return String(t("applications.errors.previewsDisabled"));
    }
    const raw = stripErrorPrefix(error.message);
    return raw
      ? `${String(t("common.errors.requestFailed"))} ${raw}`
      : String(t("common.errors.requestFailed"));
  }
  if (error instanceof Error) {
    const raw = stripErrorPrefix(error.message);
    return raw
      ? `${String(t("common.errors.unexpected"))} ${raw}`
      : String(t("common.errors.unexpected"));
  }
  return String(t("common.errors.unexpected"));
}
