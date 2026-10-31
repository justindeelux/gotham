interface ApiErrorShape {
  status?: number | null;
  message?: string;
  cause?: {
    response?: {
      data?: { code?: string; errors?: Record<string, string>; log?: string };
    };
  };
}

export function errorText(error: unknown): string {
  return (error as ApiErrorShape)?.message ?? "";
}

export function fieldErrors(error: unknown): Record<string, string> {
  return (error as ApiErrorShape)?.cause?.response?.data?.errors ?? {};
}

export function errorLog(error: unknown): string {
  return (error as ApiErrorShape)?.cause?.response?.data?.log ?? "";
}

export function errorCode(error: unknown): string {
  return (error as ApiErrorShape)?.cause?.response?.data?.code ?? "";
}

export function isForbidden(error: unknown): boolean {
  return (error as ApiErrorShape)?.status === 403;
}
