package terminal

import "time"

// WebSocket wire contract
//
// One browser frame is one message. The first byte is an opcode; the remaining
// bytes are the opcode's payload. All frames are binary. Terminal output is
// forwarded as raw bytes and must never be decoded or re-encoded by a consumer
// (xterm.js writes it directly).
//
//	client -> server
//	  0x01  input    raw keyboard/paste bytes (1..maxInputBytes)
//	  0x02  resize   exactly 4 bytes: columns and rows as uint16 big-endian
//	  0x03  close    no payload; the server tears the session down
//	  0x04  ping     no payload; client heartbeat, refreshes the idle deadline
//
//	server -> client
//	  0x01  output   raw PTY bytes (UTF-8 plus ANSI escape sequences)
//	  0x02  exited   JSON {"exit_code":<int32>,"signal":"<name>"}
//	  0x03  error    UTF-8 message, bounded; the connection closes after it
//	  0x04  opened   JSON {"shell","cols","rows","service_user"}
//	  0x05  ping     no payload; server heartbeat, sent every 25s
//
// A client must send any frame (input, resize or a 0x04 heartbeat) at least
// every 60s; the server closes a socket that goes silent (a half-open
// connection otherwise holds the PTY until the 8h cap).
//
// The stream is bound to one session and one agent stream. There is no resume
// and no input replay: a dropped WebSocket ends the PTY, and reopening means
// creating a new session. The subprotocol "gotham.terminal.v1" is advertised by
// the browser, and the access token travels in a "gotham.token.<jwt>"
// subprotocol or in an Authorization header — never in the URL.

const (
	// Subprotocol is the WebSocket subprotocol the terminal stream advertises.
	Subprotocol = "gotham.terminal.v1"
	// TokenProtocolPrefix prefixes the access token when a browser carries it as
	// a subprotocol (browsers cannot set an Authorization header on WebSocket).
	TokenProtocolPrefix = "gotham.token."

	// WebSocket opcodes.
	opInput     byte = 0x01
	opResize    byte = 0x02
	opClose     byte = 0x03
	opHeartbeat byte = 0x04
	opOutput    byte = 0x01
	opExited    byte = 0x02
	opError     byte = 0x03
	opOpened    byte = 0x04
	// opPing is the server heartbeat frame.
	opPing byte = 0x05
)

const (
	// maxInputBytes bounds one input frame from the client; it matches the
	// agent's own per-message ceiling.
	maxInputBytes = 64 << 10
	// maxWSFrame bounds one received WebSocket message (opcode included).
	maxWSFrame = maxInputBytes + 1
	// maxErrorBytes bounds an error frame.
	maxErrorBytes = 512
	// openTimeout bounds how long a session waits for the agent's opened event.
	openTimeout = 20 * time.Second
)

// openedEvent is the JSON payload of an opOpened frame.
type openedEvent struct {
	Shell       string `json:"shell"`
	Cols        uint32 `json:"cols"`
	Rows        uint32 `json:"rows"`
	ServiceUser string `json:"service_user"`
}

// exitedEvent is the JSON payload of an opExited frame.
type exitedEvent struct {
	ExitCode int32  `json:"exit_code"`
	Signal   string `json:"signal"`
}
