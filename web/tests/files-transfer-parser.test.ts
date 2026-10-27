/* global ReadableStream, TextEncoder */
import { describe, expect, it } from "vitest";

import {
  TransferIncompleteError,
  TransferStreamError,
  readTransferStream,
  transferLineLimit,
} from "@/features/files/api/transfer";
import type { TransferEvent } from "@/features/files/schemas/transfer";
import { transferPlanSchema } from "@/features/files/schemas/transfer";

function streamOf(chunks: Uint8Array[]): ReadableStream<Uint8Array> {
  let index = 0;
  return new ReadableStream<Uint8Array>({
    pull(controller) {
      if (index >= chunks.length) {
        controller.close();
        return;
      }
      controller.enqueue(chunks[index]);
      index += 1;
    },
  });
}

function bytes(text: string): Uint8Array {
  return new TextEncoder().encode(text);
}

function chunked(text: string): ReadableStream<Uint8Array> {
  const all = bytes(text);
  return streamOf(Array.from(all, (value) => new Uint8Array([value])));
}

function summaryLine(overrides: Record<string, unknown> = {}): string {
  return `${JSON.stringify({
    type: "summary",
    status: "copied",
    summary: {
      transfer_id: "t-1",
      mode: "bundle",
      status: "copied",
      copied: 1,
      conflicts: 0,
      skipped: 0,
      failed: 0,
      cancelled: 0,
      uncertain: 0,
      entries: [{ name: "report.txt", status: "copied" }],
      skipped_count: 0,
      ...overrides,
    },
  })}\n`;
}

describe("transfer NDJSON parser", () => {
  it("decodes UTF-8 split across chunk boundaries and split lines", async () => {
    const phase = JSON.stringify({
      type: "phase",
      phase: "transferring",
      bytes: 1024,
      total_bytes: 4096,
    });
    const events: TransferEvent[] = [];
    const summary = await readTransferStream(
      chunked(`${phase}\r\n${summaryLine()}`),
      (event) => events.push(event),
    );

    expect(events.map((event) => event.type)).toEqual(["phase", "summary"]);
    expect(summary.status).toBe("copied");
    expect(summary.entries[0]?.name).toBe("report.txt");
  });

  it("accepts a final line without a trailing newline", async () => {
    const summary = await readTransferStream(
      streamOf([bytes(summaryLine().trimEnd())]),
      () => undefined,
    );
    expect(summary.transfer_id).toBe("t-1");
  });

  it("keeps nested skipped samples and distinct reasons", async () => {
    const summary = await readTransferStream(
      streamOf([
        bytes(
          summaryLine({
            status: "failed",
            reason: "timeout",
            skipped_count: 2,
            skipped_samples: [{ path: "dir/secret", reason: "permission" }],
          }),
        ),
      ]),
      () => undefined,
    );
    expect(summary.reason).toBe("timeout");
    expect(summary.skipped_count).toBe(2);
    expect(summary.skipped_samples?.[0]?.path).toBe("dir/secret");
  });

  it("rejects malformed events", async () => {
    await expect(
      readTransferStream(streamOf([bytes("{not json}\n")]), () => undefined),
    ).rejects.toBeInstanceOf(TransferStreamError);
  });

  it("rejects unknown event types", async () => {
    await expect(
      readTransferStream(
        streamOf([bytes('{"type":"result","name":"a","status":"copied"}\n')]),
        () => undefined,
      ),
    ).rejects.toBeInstanceOf(TransferStreamError);
  });

  it("rejects an event after the terminal summary", async () => {
    await expect(
      readTransferStream(
        streamOf([
          bytes(
            `${summaryLine()}{"type":"phase","phase":"transferring","bytes":1}\n`,
          ),
        ]),
        () => undefined,
      ),
    ).rejects.toBeInstanceOf(TransferStreamError);
  });

  it("rejects an oversized line", async () => {
    const text = `${"x".repeat(transferLineLimit + 32)}\n`;
    await expect(
      readTransferStream(streamOf([bytes(text)]), () => undefined),
    ).rejects.toBeInstanceOf(TransferStreamError);
  });

  it("treats EOF without a summary as interrupted, never success", async () => {
    const phase = `${JSON.stringify({
      type: "phase",
      phase: "publishing",
      bytes: 4,
    })}\n`;
    await expect(
      readTransferStream(streamOf([bytes(phase)]), () => undefined),
    ).rejects.toBeInstanceOf(TransferIncompleteError);
  });
});

describe("transfer plan schema", () => {
  it("parses the real Go plan shape", () => {
    const plan = transferPlanSchema.safeParse({
      mode: "bundle",
      bounds: {
        max_bundle_bytes: 1073741824,
        max_expanded_bytes: 4294967296,
        max_entries: 100000,
        max_sources: 1024,
        max_concurrency: 4,
        bundle_available: true,
      },
      sources: [
        { name: "data", kind: "directory", size: 4096, conflict: true },
      ],
      conflicts: ["data"],
      unsupported: [],
      available_bytes: 2048,
      fits: false,
    });
    expect(plan.success).toBe(true);
  });
});
