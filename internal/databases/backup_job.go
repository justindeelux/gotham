package databases

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Framing of a temporary job's output. A dump container writes:
//
//	GOTHAM-BACKUP-START <run id>\n
//	GOTHAM-BACKUP-PAYLOAD-B64 <run id> <encoded byte count>\n
//	<base64 payload bytes>
//	GOTHAM-BACKUP-END <run id> ok\n
//
// a failed job omits the payload line and ends with "fail <status>" plus the
// tail of the engine log.
//
// The payload is length-prefixed rather than delimited, and it travels
// base64-encoded: a dump tool writes arbitrary binary bytes, and the Docker
// API serves container logs through the daemon's logging driver, which
// replaces every invalid UTF-8 byte with U+FFFD before the control plane can
// read it. Base64 keeps the wire ASCII, so the artifact survives byte for
// byte, while the declared encoded size still streams without buffering. A
// base64 frame ends exactly at its declared size: the end marker must follow
// immediately (zero separator bytes), the encoded payload itself is unwrapped
// (no CR/LF), and a job emits exactly one frame, so an under-declared payload,
// extra encoded input or a repeated same-run framing sequence is rejected
// instead of silently truncated or discarded. The plain GOTHAM-BACKUP-PAYLOAD
// marker remains supported for frames with no byte payload (restore and
// staging jobs announce zero) and for backwards-compatible callers; the
// markers carry the run id so output of an earlier container can never be
// mistaken for this one.
const (
	jobStartPrefix      = "GOTHAM-BACKUP-START "
	jobPayloadPrefix    = "GOTHAM-BACKUP-PAYLOAD "
	jobPayloadB64Prefix = "GOTHAM-BACKUP-PAYLOAD-B64 "
	jobEndPrefix        = "GOTHAM-BACKUP-END "

	// jobMarkerLimit bounds a candidate marker: a line longer than this
	// cannot be a marker, so binary noise can never grow the buffer.
	jobMarkerLimit = 4 << 10
	// jobNoiseLimit bounds the diagnostic tail kept from output outside the
	// frame (engine chatter before the start marker, tool output after the
	// end marker).
	jobNoiseLimit = 8 << 10
	// jobDiagLimit bounds the diagnostic text embedded in an error message.
	jobDiagLimit = 600
)

// jobState is the position of the collector inside the frame.
type jobState int

const (
	// jobSeekStart — reading lines until the start marker appears.
	jobSeekStart jobState = iota
	// jobSeekPayload — start marker seen, awaiting the payload header.
	jobSeekPayload
	// jobReadPayload — copying exactly the announced byte count to dst.
	jobReadPayload
	// jobSeekEnd — payload complete, awaiting the end marker.
	jobSeekEnd
	// jobDone — the frame is complete (ok or failed).
	jobDone
)

// jobCollector frames one temporary container's log stream. It is an io.Writer
// so the job runner can feed it straight from the log channel, and it never
// buffers more than a marker-sized line plus the payload currently in flight
// — a multi-gigabyte dump streams straight through to dst.
type jobCollector struct {
	runID string
	dst   io.Writer

	state   jobState
	buf     []byte
	remain  int64
	written int64

	// base64 marks a payload that travels encoded (the dump jobs): the bytes
	// are decoded while they stream to dst. carry holds the fewer-than-four
	// trailing characters of an unfinished quantum between writes, and padded
	// records that a quantum ended the encoded stream, so no further encoded
	// byte may follow regardless of how Docker splits the writes.
	base64 bool
	carry  []byte
	padded bool

	// scan carries the tail of post-frame output between writes so a second
	// same-run framing sequence is detected even when it is split across
	// chunks. It never holds more than the start marker's length.
	scan []byte

	ok     bool
	status string
	diag   []byte
	err    error
}

// newJobCollector returns a collector that writes the framed payload of runID
// into dst. A nil dst discards the payload (restore jobs only need the
// markers).
func newJobCollector(runID string, dst io.Writer) *jobCollector {
	return &jobCollector{runID: runID, dst: dst, state: jobSeekStart}
}

// Write feeds the next slice of the log stream into the state machine. It
// never fails on framing problems: a broken frame is reported by Result, so a
// caller can always drain the channel to the end.
func (c *jobCollector) Write(p []byte) (int, error) {
	if c.err != nil {
		return len(p), nil
	}
	c.buf = append(c.buf, p...)
	for c.step() {
		// Advance the state machine until it needs more bytes.
	}
	return len(p), nil
}

// step advances the state machine by one unit and reports whether it moved.
func (c *jobCollector) step() bool {
	switch c.state {
	case jobSeekStart:
		line, ok := c.takeLine()
		if !ok {
			return false
		}
		if strings.TrimSuffix(line, "\r") == jobStartPrefix+c.runID {
			c.state = jobSeekPayload
		} else if line != "" {
			c.keepNoise(line)
		}
		return true

	case jobSeekPayload:
		line, ok := c.takeLine()
		if !ok {
			return false
		}
		switch {
		case strings.HasPrefix(line, jobPayloadB64Prefix+c.runID):
			c.base64 = true
			c.startPayload(strings.TrimPrefix(line, jobPayloadB64Prefix+c.runID))
		case strings.HasPrefix(line, jobPayloadPrefix+c.runID):
			c.startPayload(strings.TrimPrefix(line, jobPayloadPrefix+c.runID))
		case strings.HasPrefix(line, jobEndPrefix+c.runID):
			c.readEnd(line)
		case line != "":
			// Anything else between the markers is engine chatter; keep it
			// for diagnostics and keep waiting for the payload header.
			c.keepNoise(line)
		}
		return true

	case jobReadPayload:
		if len(c.buf) == 0 {
			return false
		}
		n := int64(len(c.buf))
		if n > c.remain {
			n = c.remain
		}
		chunk := c.buf[:n]
		c.buf = c.buf[n:]
		c.remain -= n
		if err := c.writePayload(chunk); err != nil {
			c.err = err
			c.state = jobDone
			return true
		}
		if c.remain == 0 {
			if c.base64 && len(c.carry) != 0 {
				c.err = fmt.Errorf("databases: base64 job payload ended inside a quantum")
				c.state = jobDone
				return true
			}
			c.state = jobSeekEnd
		}
		return true

	case jobSeekEnd:
		// A base64 payload ends at its declared size, so the end marker must
		// follow immediately; only raw zero-payload frames (restore, staging)
		// tolerate engine chatter here.
		if c.base64 && !c.matchEndPrefix() {
			return false
		}
		line, ok := c.takeLine()
		if !ok {
			return false
		}
		if strings.HasPrefix(line, jobEndPrefix+c.runID) {
			c.readEnd(line)
		} else if line != "" {
			c.keepNoise(line)
		}
		return true

	case jobDone:
		if len(c.buf) > 0 {
			if c.detectSecondFrame(c.buf) {
				c.err = fmt.Errorf("databases: job emitted a second completion frame")
				c.buf = nil
				c.scan = nil
				return false
			}
			c.keepNoise(string(c.buf))
			c.buf = nil
		}
		return false
	}
	return false
}

// matchEndPrefix enforces the base64 frame contract: the declared payload is
// followed immediately by the end marker, with no separator or extra bytes.
// It reports false while more bytes are needed and records a framing error on
// the first mismatching byte, so an under-declared payload that hides extra
// encoded input can never complete successfully.
func (c *jobCollector) matchEndPrefix() bool {
	prefix := []byte(jobEndPrefix + c.runID)
	if len(c.buf) < len(prefix) {
		if !bytes.HasPrefix(prefix, c.buf) {
			c.err = fmt.Errorf("databases: base64 job payload is not followed by the completion marker")
			c.state = jobDone
		}
		return false
	}
	if !bytes.HasPrefix(c.buf, prefix) {
		c.err = fmt.Errorf("databases: base64 job payload is not followed by the completion marker")
		c.state = jobDone
		return false
	}
	return true
}

// startPayload parses the byte count of a payload header and enters the
// payload state. A missing, malformed or negative count fails the frame.
func (c *jobCollector) startPayload(raw string) {
	size := strings.TrimSpace(raw)
	n, err := strconv.ParseInt(size, 10, 64)
	if err != nil || n < 0 {
		c.err = fmt.Errorf("databases: job reported an invalid payload size %q", size)
		c.state = jobDone
		return
	}
	c.remain = n
	c.state = jobReadPayload
}

// writePayload forwards one slice of the announced payload. Raw frames go
// straight to dst; base64 frames are decoded in bounded quanta first, keeping
// memory at one chunk plus fewer than four carry characters. Corrupt base64
// fails the frame, so a mangled stream can never be accepted as an artifact.
func (c *jobCollector) writePayload(chunk []byte) error {
	if !c.base64 {
		if c.dst != nil && len(chunk) > 0 {
			if _, err := c.dst.Write(chunk); err != nil {
				return fmt.Errorf("databases: write job payload: %w", err)
			}
		}
		c.written += int64(len(chunk))
		return nil
	}
	// The wire format is explicitly unwrapped: the declared payload must not
	// contain CR/LF, which the standard decoder would silently ignore in one
	// batch and the padding state would reject in another. Rejecting every
	// payload chunk here keeps validity independent of how Docker splits the
	// writes.
	if bytes.ContainsAny(chunk, "\r\n") {
		return fmt.Errorf("databases: base64 job payload contains a line break")
	}
	c.carry = append(c.carry, chunk...)
	full := len(c.carry) - len(c.carry)%4
	if full == 0 {
		return nil
	}
	if c.padded {
		return fmt.Errorf("databases: base64 job payload continues after padding")
	}
	batch := c.carry[:full]
	decoded := make([]byte, base64.StdEncoding.DecodedLen(full))
	count, err := base64.StdEncoding.Decode(decoded, batch)
	if err != nil {
		return fmt.Errorf("databases: decode base64 job payload: %w", err)
	}
	if bytes.IndexByte(batch, '=') >= 0 {
		c.padded = true
	}
	if c.dst != nil && count > 0 {
		if _, err := c.dst.Write(decoded[:count]); err != nil {
			return fmt.Errorf("databases: write job payload: %w", err)
		}
	}
	c.written += int64(count)
	c.carry = append(c.carry[:0], c.carry[full:]...)
	return nil
}

// detectSecondFrame reports whether another same-run start marker appears in
// tail once the bytes carried from previous writes are prepended. Real jobs
// emit exactly one frame, so a second start/payload/end sequence is a producer
// violation; it fails the frame while bounded post-frame diagnostics remain
// allowed.
func (c *jobCollector) detectSecondFrame(tail []byte) bool {
	needle := jobStartPrefix + c.runID
	scan := append(c.scan, tail...)
	if bytes.Contains(scan, []byte(needle)) {
		return true
	}
	if keep := len(needle) - 1; len(scan) > keep {
		scan = scan[len(scan)-keep:]
	}
	c.scan = append(c.scan[:0], scan...)
	return false
}

// takeLine removes the first complete line from the buffer. ok is false when
// no line is available yet; runs longer than jobMarkerLimit are treated as
// noise so binary output cannot pin memory.
func (c *jobCollector) takeLine() (line string, ok bool) {
	if i := bytes.IndexByte(c.buf, '\n'); i >= 0 {
		line = string(c.buf[:i])
		c.buf = c.buf[i+1:]
		return line, true
	}
	if len(c.buf) > jobMarkerLimit {
		// Not a marker (markers are far shorter than the limit): treat the
		// whole run as noise instead of waiting for a newline that may be
		// megabytes away.
		c.keepNoise(string(c.buf))
		c.buf = c.buf[:0]
	}
	return "", false
}

// readEnd parses a complete end marker line.
func (c *jobCollector) readEnd(line string) {
	rest := strings.TrimSpace(strings.TrimPrefix(line, jobEndPrefix+c.runID))
	switch {
	case strings.HasPrefix(rest, "ok"):
		c.ok = true
	case strings.HasPrefix(rest, "fail"):
		c.ok = false
		fields := strings.Fields(rest)
		if len(fields) > 1 {
			c.status = fields[1]
		}
	default:
		c.ok = false
		c.status = "unknown"
	}
	c.state = jobDone
}

// keepNoise appends text to the bounded diagnostic tail.
func (c *jobCollector) keepNoise(text string) {
	c.diag = append(c.diag, text...)
	c.diag = append(c.diag, '\n')
	if len(c.diag) > jobNoiseLimit {
		c.diag = c.diag[len(c.diag)-jobNoiseLimit:]
	}
}

// Diagnostics returns the bounded tail of output outside the frame.
func (c *jobCollector) Diagnostics() string {
	return strings.TrimSpace(string(c.diag))
}

// Written reports how many payload bytes were written to dst.
func (c *jobCollector) Written() int64 { return c.written }

// Result validates the frame and reports the job's outcome: nil when the end
// marker says ok, an error carrying the diagnostics otherwise.
func (c *jobCollector) Result() error {
	if c.err != nil {
		return c.err
	}
	diag := c.Diagnostics()
	switch {
	case c.state != jobDone:
		if diag == "" {
			return fmt.Errorf("databases: temporary container exited without a completion marker")
		}
		return fmt.Errorf("databases: temporary container exited without a completion marker: %s",
			boundedDiag(diag))
	case !c.ok:
		if c.status != "" && diag != "" {
			return fmt.Errorf("databases: temporary container failed with status %s: %s",
				c.status, boundedDiag(diag))
		}
		if c.status != "" {
			return fmt.Errorf("databases: temporary container failed with status %s", c.status)
		}
		if diag != "" {
			return fmt.Errorf("databases: temporary container failed: %s", boundedDiag(diag))
		}
		return fmt.Errorf("databases: temporary container failed")
	default:
		return nil
	}
}

// boundedDiag trims a diagnostic tail to jobDiagLimit characters so an error
// stays usable in a log line, an API message and a database column.
func boundedDiag(diag string) string {
	diag = strings.ReplaceAll(diag, "\x00", "")
	diag = strings.TrimSpace(diag)
	if len(diag) <= jobDiagLimit {
		return diag
	}
	return "…" + diag[len(diag)-jobDiagLimit:]
}
