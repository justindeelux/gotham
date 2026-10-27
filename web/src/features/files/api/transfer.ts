import { getAccessToken } from "@/shared/api/token";
import { http, refreshSession } from "@/shared/api/http";
import type { ApiError } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import type {
  TransferEvent,
  TransferPlan,
  TransferRequest,
  TransferSummary,
} from "@/features/files/schemas/transfer";
import {
  transferEventSchema,
  transferPlanSchema,
} from "@/features/files/schemas/transfer";

/** A single NDJSON line is bounded so a broken peer cannot exhaust memory. */
export const transferLineLimit = 4 * 1024 * 1024;

export class TransferStreamError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "TransferStreamError";
  }
}

/** The stream ended without the one terminal summary event. */
export class TransferIncompleteError extends TransferStreamError {
  constructor() {
    super("transfer stream ended without a summary");
    this.name = "TransferIncompleteError";
  }
}

export function isTransferIncomplete(error: unknown): boolean {
  return error instanceof TransferIncompleteError;
}

export function isTransferAborted(error: unknown): boolean {
  return (
    (error instanceof DOMException && error.name === "AbortError") ||
    (error instanceof Error && error.name === "AbortError")
  );
}

export async function planTransfer(
  serverId: string,
  request: TransferRequest,
  signal?: AbortSignal,
): Promise<TransferPlan> {
  const response = await http.post(
    `/servers/${serverId}/files/transfers/plan`,
    request,
    { signal, timeout: 0 },
  );
  return parseWith(transferPlanSchema, response.data, {
    context: "FileTransferPlan",
  });
}

function apiError(
  status: number | null,
  message: string,
  code: string | undefined,
  cause: unknown,
): ApiError {
  return { status, message, code, cause };
}

async function errorFromResponse(response: Response): Promise<ApiError> {
  let message = `Transfer failed: ${response.status}`;
  let code: string | undefined;
  try {
    const body = (await response.json()) as {
      message?: unknown;
      code?: unknown;
    };
    if (typeof body.message === "string" && body.message !== "") {
      message = body.message;
    }
    if (typeof body.code === "string") {
      code = body.code;
    }
  } catch {
    // Keep the status-only message when the error body is not JSON.
  }
  return apiError(response.status, message, code, null);
}

async function openTransferStream(
  serverId: string,
  request: TransferRequest,
  signal?: AbortSignal,
): Promise<Response> {
  const send = async (): Promise<Response> => {
    const token = getAccessToken();
    return fetch(`/api/v1/servers/${serverId}/files/transfers`, {
      method: "POST",
      headers: {
        Accept: "application/x-ndjson",
        "Content-Type": "application/json",
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      body: JSON.stringify(request),
      signal,
    });
  };

  let response = await send();
  if (response.status === 401) {
    await refreshSession();
    response = await send();
  }
  if (!response.ok) {
    throw await errorFromResponse(response);
  }
  return response;
}

function parseLine(line: string): TransferEvent {
  let data: unknown;
  try {
    data = JSON.parse(line);
  } catch {
    throw new TransferStreamError("transfer event is not valid JSON");
  }
  const parsed = transferEventSchema.safeParse(data);
  if (!parsed.success) {
    throw new TransferStreamError("transfer event has an unknown shape");
  }
  return parsed.data;
}

/**
 * Reads the incremental NDJSON body. Decodes UTF-8 across chunk boundaries,
 * accepts split and blank lines, rejects oversized lines and unknown events,
 * and treats a missing terminal summary as an interrupted stream rather than
 * success.
 */
export async function readTransferStream(
  body: ReadableStream<Uint8Array>,
  onEvent: (_event: TransferEvent) => void,
): Promise<TransferSummary> {
  const reader = body.getReader();
  const decoder = new TextDecoder("utf-8");
  let buffer = "";
  let summary: TransferSummary | null = null;
  let completed = false;

  const consume = (line: string): void => {
    const trimmed = line.replace(/\r$/, "");
    if (trimmed.trim() === "") {
      return;
    }
    if (trimmed.length > transferLineLimit) {
      throw new TransferStreamError("transfer event is too large");
    }
    if (summary !== null) {
      throw new TransferStreamError("transfer event followed the summary");
    }
    const event = parseLine(trimmed);
    if (event.type === "summary") {
      summary = event.summary;
    }
    onEvent(event);
  };

  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) {
        buffer += decoder.decode();
        break;
      }
      buffer += decoder.decode(value, { stream: true });
      let newline = buffer.indexOf("\n");
      while (newline >= 0) {
        const line = buffer.slice(0, newline);
        buffer = buffer.slice(newline + 1);
        consume(line);
        newline = buffer.indexOf("\n");
      }
      if (buffer.length > transferLineLimit) {
        throw new TransferStreamError("transfer event is too large");
      }
    }
    consume(buffer);
    completed = true;
  } finally {
    if (!completed) {
      try {
        await reader.cancel();
      } catch {
        // The body may already be closed or aborted.
      }
    }
    reader.releaseLock();
  }

  if (summary === null) {
    throw new TransferIncompleteError();
  }
  return summary;
}

export interface StreamTransferOptions {
  signal?: AbortSignal;
  onEvent?: (_event: TransferEvent) => void;
}

/** Runs the copy and resolves only after the destination sent its summary. */
export async function streamTransfer(
  serverId: string,
  request: TransferRequest,
  options: StreamTransferOptions = {},
): Promise<TransferSummary> {
  const response = await openTransferStream(serverId, request, options.signal);
  if (response.body === null) {
    throw new TransferStreamError("transfer response has no body");
  }
  return readTransferStream(response.body, options.onEvent ?? (() => undefined));
}

export function isTransferStreamError(error: unknown): boolean {
  return error instanceof TransferStreamError;
}
