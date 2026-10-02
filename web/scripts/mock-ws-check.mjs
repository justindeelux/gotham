// Mock realtime-hub integration check for web/src/composables/useWebSocket.ts.
//
// The repo has no web test runner (no vitest/jsdom), so this script stands in
// for a unit test: it bundles the composable with the project's own esbuild,
// starts a minimal RFC 6455 WebSocket server on a random port, then exercises
// the merged BE-3.2 contract end to end.
//
// Run from web/:  node scripts/mock-ws-check.mjs

import { createHash } from "node:crypto";
import { mkdtemp, rm } from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

import { build } from "esbuild";

const wsGuid = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11";

// ── tiny assertion harness ───────────────────────────────────────────────
const results = [];

async function check(name, fn) {
  try {
    await fn();
    results.push({ name, ok: true });
    console.log(`  ✓ ${name}`);
  } catch (error) {
    results.push({ name, ok: false, error });
    console.error(`  ✗ ${name}\n      ${error.message}`);
  }
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(message ?? "assertion failed");
  }
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitFor(predicate, { timeout = 2000, interval = 10 } = {}) {
  const start = Date.now();
  while (Date.now() - start < timeout) {
    if (predicate()) {
      return;
    }
    await sleep(interval);
  }
  throw new Error("timed out waiting for condition");
}

// ── minimal RFC 6455 server ──────────────────────────────────────────────
function acceptKey(key) {
  return createHash("sha1").update(key + wsGuid).digest("base64");
}

function encodeTextFrame(text) {
  const payload = Buffer.from(text, "utf8");
  const length = payload.length;
  let header;
  if (length < 126) {
    header = Buffer.from([0x81, length]);
  } else if (length < 65536) {
    header = Buffer.alloc(4);
    header[0] = 0x81;
    header[1] = 126;
    header.writeUInt16BE(length, 2);
  } else {
    header = Buffer.alloc(10);
    header[0] = 0x81;
    header[1] = 127;
    header.writeBigUInt64BE(BigInt(length), 2);
  }
  return Buffer.concat([header, payload]);
}

function encodeCloseFrame(code = 1000) {
  const payload = Buffer.alloc(2);
  payload.writeUInt16BE(code, 0);
  return Buffer.concat([Buffer.from([0x88, 0x02]), payload]);
}

function createFrameParser(onText, onClose) {
  let buffer = Buffer.alloc(0);
  return (chunk) => {
    buffer = Buffer.concat([buffer, chunk]);
    for (;;) {
      if (buffer.length < 2) return;
      const opcode = buffer[0] & 0x0f;
      const masked = (buffer[1] & 0x80) !== 0;
      let length = buffer[1] & 0x7f;
      let offset = 2;
      if (length === 126) {
        if (buffer.length < 4) return;
        length = buffer.readUInt16BE(2);
        offset = 4;
      } else if (length === 127) {
        if (buffer.length < 10) return;
        length = Number(buffer.readBigUInt64BE(2));
        offset = 10;
      }
      let maskKey = null;
      if (masked) {
        if (buffer.length < offset + 4) return;
        maskKey = buffer.subarray(offset, offset + 4);
        offset += 4;
      }
      if (buffer.length < offset + length) return;
      let payload = buffer.subarray(offset, offset + length);
      if (maskKey) {
        const unmasked = Buffer.alloc(length);
        for (let i = 0; i < length; i += 1) {
          unmasked[i] = payload[i] ^ maskKey[i % 4];
        }
        payload = unmasked;
      }
      buffer = buffer.subarray(offset + length);
      if (opcode === 0x01) {
        onText(payload.toString("utf8"));
      } else if (opcode === 0x08) {
        onClose();
        return;
      }
    }
  };
}

async function startMockServer() {
  const state = {
    connections: 0,
    requests: [],
    messages: [],
    clients: [],
  };

  const server = createServer();
  server.on("upgrade", (request, socket) => {
    state.connections += 1;
    state.requests.push(request.url);
    const key = request.headers["sec-websocket-key"];
    socket.write(
      "HTTP/1.1 101 Switching Protocols\r\n" +
        "Upgrade: websocket\r\n" +
        "Connection: Upgrade\r\n" +
        `Sec-WebSocket-Accept: ${acceptKey(key)}\r\n\r\n`,
    );

    const client = {
      socket,
      send: (text) => socket.write(encodeTextFrame(text)),
      close: (code) => socket.write(encodeCloseFrame(code)),
    };
    state.clients.push(client);

    socket.on(
      "data",
      createFrameParser(
        (text) => state.messages.push(text),
        () => {
          socket.end();
        },
      ),
    );
    socket.on("close", () => {
      // keep the client entry: tests need connection counts, not liveness.
    });
    socket.on("error", () => {});
  });

  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address();

  return {
    state,
    port,
    url: `ws://127.0.0.1:${port}/api/v1/ws`,
    latest: () => state.clients[state.clients.length - 1],
    close: async () => {
      for (const client of state.clients) {
        client.socket.destroy();
      }
      await new Promise((resolve) => server.close(resolve));
    },
  };
}

const subscribeFrames = (state) =>
  state.messages.filter((raw) => {
    try {
      return typeof JSON.parse(raw).subscribe === "string";
    } catch {
      return false;
    }
  });

// ── load a TypeScript module through esbuild ─────────────────────────────
async function loadModule(relativePath) {
  const directory = await mkdtemp(join(tmpdir(), "gotham-ws-check-"));
  const outfile = join(directory, "module.mjs");
  await build({
    entryPoints: [new URL(relativePath, import.meta.url).pathname],
    outfile,
    bundle: true,
    format: "esm",
    platform: "node",
    target: "node20",
    logLevel: "silent",
  });
  const module = await import(pathToFileURL(outfile).href);
  return {
    module,
    cleanup: () => rm(directory, { recursive: true, force: true }),
  };
}

async function main() {
  if (typeof WebSocket === "undefined") {
    throw new Error("global WebSocket is unavailable; Node 22+ is required");
  }

  const composable = await loadModule("../src/composables/useWebSocket.ts");
  const channelBuffers = await loadModule(
    "../src/composables/logChannelBuffers.ts",
  );
  const {
    useWebSocket,
    computeBackoffDelay,
    buildWebSocketUrl,
  } = composable.module;
  const {
    activateChannel,
    createChannelLogBufferStore,
    flushPendingLines,
    isFrameForChannel,
  } = channelBuffers.module;

  console.log("pure helpers");
  await check("backoff doubles then caps", () => {
    assert(computeBackoffDelay(1, 500, 15000, false) === 500, "attempt 1");
    assert(computeBackoffDelay(2, 500, 15000, false) === 1000, "attempt 2");
    assert(computeBackoffDelay(3, 500, 15000, false) === 2000, "attempt 3");
    assert(computeBackoffDelay(10, 500, 15000, false) === 15000, "cap");
  });
  await check("jitter stays within half-to-full cap", () => {
    assert(computeBackoffDelay(3, 500, 15000, true, () => 0) === 1000, "low");
    assert(computeBackoffDelay(3, 500, 15000, true, () => 1) === 2000, "high");
  });
  await check("url keeps path and appends token", () => {
    assert(
      buildWebSocketUrl("ws://host/api/v1/ws", "abc") ===
        "ws://host/api/v1/ws?token=abc",
      "plain",
    );
    assert(
      buildWebSocketUrl("ws://host/api/v1/ws?x=1", "abc") ===
        "ws://host/api/v1/ws?x=1&token=abc",
      "existing query",
    );
    assert(
      buildWebSocketUrl("http://host/api/v1/ws", "abc") ===
        "ws://host/api/v1/ws?token=abc",
      "http upgraded",
    );
  });

  console.log("live contract (mock hub)");
  const server = await startMockServer();
  const statuses = [];
  const client = useWebSocket({
    url: server.url,
    token: "test-jwt",
    baseDelayMs: 30,
    maxDelayMs: 60,
    maxRetries: 3,
    jitter: false,
    bufferLimit: 5,
    onStatusChange: (status) => statuses.push(status),
  });

  await check("connects with token query param", async () => {
    client.connect();
    await waitFor(() => client.status.value === "open");
    assert(
      server.state.requests[0] === "/api/v1/ws?token=test-jwt",
      `request was ${server.state.requests[0]}`,
    );
    assert(statuses.includes("connecting"), "emitted connecting");
  });

  await check("subscribe frame matches merged BE-3.2 shape", async () => {
    client.subscribe("logs:srv-1:ctr-9");
    await waitFor(() => subscribeFrames(server.state).length === 1);
    const frame = JSON.parse(subscribeFrames(server.state)[0]);
    assert(frame.subscribe === "logs:srv-1:ctr-9", "subscribe channel");
    assert(frame.channel === undefined, "no draft channel field");
  });

  await check("data and disconnect frames are classified and buffered", async () => {
    server.latest().send(
      JSON.stringify({
        channel: "logs:srv-1:ctr-9",
        type: "log",
        data: "hello\nworld\n",
      }),
    );
    server.latest().send(
      JSON.stringify({
        channel: "logs:srv-1:ctr-9",
        type: "disconnect",
        data: "stream lost",
      }),
    );
    server.latest().send(
      JSON.stringify({ channel: "logs:srv-1:ctr-9", type: "subscribed" }),
    );
    await waitFor(() => client.messages.value.length >= 3);
    const data = client.messages.value[0];
    const notice = client.messages.value[1];
    const ack = client.messages.value[2];
    assert(data.kind === "data", "data kind");
    assert(data.channel === "logs:srv-1:ctr-9", "data channel");
    assert(data.payload.data === "hello\nworld\n", "chunk preserved");
    assert(notice.kind === "notice", "disconnect notice kind");
    assert(notice.payload.data === "stream lost", "notice reason");
    assert(ack.kind === "unknown", "subscription ack not treated as log data");
  });

  await check("tolerates the pre-merge draft frame shape", async () => {
    server.latest().send(JSON.stringify({ channel: "c", chunk: "draft\n" }));
    server.latest().send(JSON.stringify({ type: "notice", message: "draft lost" }));
    await waitFor(() => client.messages.value.length === 5);
    const draftData = client.messages.value[3];
    const draftNotice = client.messages.value[4];
    assert(draftData.kind === "data", "draft chunk still data");
    assert(draftNotice.kind === "notice", "draft notice still notice");
  });

  await check("buffer drops oldest frames past the limit", async () => {
    for (let i = 0; i < 6; i += 1) {
      server
        .latest()
        .send(JSON.stringify({ channel: "c", type: "log", data: `line-${i}` }));
    }
    await waitFor(() =>
      client.messages.value.some((m) => m.payload?.data === "line-5"),
    );
    assert(client.messages.value.length === 5, "buffer capped at the limit");
    const texts = client.messages.value.map(
      (m) => m.payload?.data ?? m.payload?.message,
    );
    assert(!texts.includes("line-0"), "oldest dropped");
    assert(texts.includes("line-5"), "newest kept");
  });

  await check("stream loss triggers reconnect and re-subscribe", async () => {
    server.latest().close(1011);
    await waitFor(() => server.state.connections >= 2);
    await waitFor(() => client.status.value === "open");
    await waitFor(() => subscribeFrames(server.state).length >= 2);
    assert(
      subscribeFrames(server.state).every(
        (raw) => JSON.parse(raw).subscribe === "logs:srv-1:ctr-9",
      ),
      "re-subscribed to same channel",
    );
  });

  await check("explicit close stops reconnecting", async () => {
    const before = server.state.connections;
    client.close();
    assert(client.status.value === "closed", "status closed");
    await sleep(250);
    assert(
      server.state.connections === before,
      "no reconnect after explicit close",
    );
  });

  await check("max retries settle into the error state", async () => {
    const dead = await startMockServer();
    const deadUrl = dead.url;
    await dead.close(); // port now refuses connections
    const failing = useWebSocket({
      url: deadUrl,
      token: "t",
      baseDelayMs: 20,
      maxDelayMs: 40,
      maxRetries: 2,
      jitter: false,
    });
    failing.connect();
    await waitFor(() => failing.status.value === "error", { timeout: 2000 });
    assert(failing.retryCount.value === 2, "used the full retry budget");
    assert(failing.lastError.value !== null, "surfaced an error message");
    await sleep(150);
    assert(failing.status.value === "error", "stayed in the error state");
    assert(failing.messages.value.length === 0, "no fabricated frames");
  });

  // ── FX-12b regressions ─────────────────────────────────────────────────
  console.log("token refresh on reconnect (B2-3)");
  await check("a reconnect re-resolves the token getter", async () => {
    const tokenServer = await startMockServer();
    let token = "expired-jwt";
    const client2 = useWebSocket({
      url: tokenServer.url,
      token: () => token,
      baseDelayMs: 10,
      maxDelayMs: 20,
      maxRetries: 3,
      jitter: false,
    });
    try {
      client2.connect();
      await waitFor(() => client2.status.value === "open");
      assert(
        tokenServer.state.requests[0] === "/api/v1/ws?token=expired-jwt",
        `first request was ${tokenServer.state.requests[0]}`,
      );
      token = "refreshed-jwt";
      tokenServer.latest().close(1011);
      await waitFor(() => tokenServer.state.requests.length >= 2);
      assert(
        tokenServer.state.requests[1] === "/api/v1/ws?token=refreshed-jwt",
        `reconnect request was ${tokenServer.state.requests[1]}`,
      );
    } finally {
      client2.close();
      await tokenServer.close();
    }
  });

  console.log("denied frames (B2-6)");
  await check("a denied frame is delivered as a typed error", async () => {
    const deniedServer = await startMockServer();
    const seen = [];
    const client3 = useWebSocket({
      url: deniedServer.url,
      token: "t",
      baseDelayMs: 10,
      maxDelayMs: 20,
      maxRetries: 1,
      onMessage: (message) => seen.push(message),
    });
    try {
      client3.connect();
      await waitFor(() => client3.status.value === "open");
      deniedServer
        .latest()
        .send(
          JSON.stringify({ channel: "logs:s:d", type: "denied", data: "cap" }),
        );
      await waitFor(() => seen.length >= 1);
      assert(seen[0].kind === "denied", `kind was ${seen[0].kind}`);
      assert(seen[0].payload?.data === "cap", "server reason preserved");
      assert(client3.messages.value.length === 1, "not silently dropped");
    } finally {
      client3.close();
      await deniedServer.close();
    }
  });

  console.log("buffer byte cap (B2-9)");
  await check("the buffer is byte-capped as well as frame-capped", async () => {
    const capServer = await startMockServer();
    const client4 = useWebSocket({
      url: capServer.url,
      token: "t",
      baseDelayMs: 10,
      maxDelayMs: 20,
      maxRetries: 1,
      bufferLimit: 100,
      bufferByteLimit: 500,
    });
    try {
      client4.connect();
      await waitFor(() => client4.status.value === "open");
      const chunk = "x".repeat(200);
      for (let i = 0; i < 5; i += 1) {
        capServer
          .latest()
          .send(
            JSON.stringify({ channel: "c", type: "log", data: `${chunk}${i}` }),
          );
      }
      await waitFor(
        () =>
          client4.messages.value.length >= 1 &&
          client4.messages.value.length < 5,
      );
      const bytes = client4.messages.value.reduce((n, m) => n + m.raw.length, 0);
      assert(bytes <= 500, `buffered ${bytes} bytes over the cap`);
      assert(client4.messages.value.length >= 1, "kept the newest frame");
    } finally {
      client4.close();
      await capServer.close();
    }
  });

  console.log("single socket invariant (B2-10)");
  await check("a re-entrant connect closes the superseded socket", async () => {
    const RealWebSocket = globalThis.WebSocket;
    const instances = [];
    class FakeWebSocket {
      constructor(url) {
        this.url = url;
        this.readyState = 0;
        this.closed = false;
        instances.push(this);
      }
      close() {
        this.closed = true;
        this.readyState = 3;
      }
      send() {}
    }
    globalThis.WebSocket = FakeWebSocket;
    let client5 = null;
    try {
      client5 = useWebSocket({
        url: "ws://fake/api/v1/ws",
        token: "t",
        baseDelayMs: 10,
        maxDelayMs: 20,
        maxRetries: 5,
        jitter: false,
      });
      client5.connect();
      assert(instances.length === 1, "first socket opened");
      instances[0].onerror?.();
      await waitFor(() => instances.length === 2, { timeout: 1000 });
      assert(!instances[1].closed, "retry socket live before the re-entrant connect");
      client5.connect();
      assert(instances.length === 3, "re-entrant connect opened a socket");
      assert(instances[1].closed === true, "superseded socket was closed");
    } finally {
      client5?.close();
      globalThis.WebSocket = RealWebSocket;
    }
  });

  console.log("reconnect session refresh (U4)");
  await check("a failing reconnect refreshes the session once per streak", async () => {
    const dead = await startMockServer();
    const deadUrl = dead.url;
    await dead.close(); // port now refuses connections
    let refreshes = 0;
    const failing = useWebSocket({
      url: deadUrl,
      token: "t",
      baseDelayMs: 10,
      maxDelayMs: 20,
      maxRetries: 3,
      jitter: false,
      onReconnectFailed: () => {
        refreshes += 1;
      },
    });
    try {
      failing.connect();
      await waitFor(() => failing.status.value === "error", { timeout: 3000 });
      assert(failing.retryCount.value === 3, "used the full retry budget");
      assert(refreshes === 1, `session refresh ran ${refreshes} times`);
    } finally {
      failing.close();
    }
  });

  console.log("per-channel log buffers (B2-2 / C4-4)");
  await check("switching channel scopes the rendered buffer", () => {
    const store = createChannelLogBufferStore();
    const a = { lines: [{ id: 1 }], pending: [] };
    const b = activateChannel(store, "chan-a", "chan-b", a);
    assert(b.lines.length === 0, "a new channel starts empty");
    b.lines.push({ id: 2 });
    const back = activateChannel(store, "chan-b", "chan-a", b);
    assert(back === a, "a seen channel reuses its buffer");
    assert(
      back.lines.length === 1 && back.lines[0].id === 1,
      "the previous channel's lines are preserved, not concatenated",
    );
    assert(
      b.lines.length === 1 && b.lines[0].id === 2,
      "the second channel keeps its own line",
    );
  });
  await check("frames from the previous channel are dropped", () => {
    assert(isFrameForChannel("chan-a", "chan-b") === false, "stale dropped");
    assert(isFrameForChannel("chan-b", "chan-b") === true, "active kept");
    assert(isFrameForChannel(null, "chan-b") === true, "channel-less kept");
  });
  await check("a restored pending queue flushes in order", () => {
    const buffer = { lines: [{ id: 1 }], pending: [{ id: 2 }, { id: 3 }] };
    flushPendingLines(buffer, 10);
    assert(buffer.pending.length === 0, "pending drained");
    assert(
      buffer.lines.map((line) => line.id).join(",") === "1,2,3",
      "merged in order",
    );
    const capped = { lines: [{ id: 1 }], pending: [{ id: 2 }, { id: 3 }] };
    flushPendingLines(capped, 2);
    assert(capped.lines.length === 2, "capped at maxLines");
    assert(
      capped.lines.map((line) => line.id).join(",") === "2,3",
      "oldest dropped",
    );
  });

  await server.close();
  await composable.cleanup();
  await channelBuffers.cleanup();

  const failed = results.filter((r) => !r.ok);
  console.log(
    `\n${results.length - failed.length}/${results.length} checks passed`,
  );
  if (failed.length > 0) {
    process.exitCode = 1;
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
