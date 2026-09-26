import axios from "axios";
import type { AxiosError, AxiosInstance, AxiosResponse } from "axios";

/** Axios error payloads returned by the Gotham API. */
interface ApiErrorBody {
  message?: string;
}

/** Normalised failure surfaced to callers of the Gotham API. */
export interface ApiError {
  status: number | null;
  message: string;
  cause: unknown;
}

/** Request timeout in milliseconds. */
const requestTimeout = 15_000;

/** Shared axios instance for the `/api/v1` control-plane API. */
export const http: AxiosInstance = axios.create({
  baseURL: "/api/v1",
  timeout: requestTimeout,
  headers: { Accept: "application/json" },
});

http.interceptors.response.use(
  (response: AxiosResponse) => response,
  (error: AxiosError<ApiErrorBody>) => Promise.reject(toApiError(error)),
);

/** toApiError maps an axios failure to a typed ApiError. */
function toApiError(error: AxiosError<ApiErrorBody>): ApiError {
  const message =
    error.response?.data?.message ?? error.message ?? "Unexpected request error";

  return {
    status: error.response?.status ?? null,
    message,
    cause: error,
  };
}
