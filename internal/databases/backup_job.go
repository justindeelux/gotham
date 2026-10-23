package databases

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	jobStartPrefix      = "GOTHAM-BACKUP-START "
	jobPayloadPrefix    = "GOTHAM-BACKUP-PAYLOAD "
	jobPayloadB64Prefix = "GOTHAM-BACKUP-PAYLOAD-B64 "
	jobEndPrefix        = "GOTHAM-BACKUP-END "

	jobMarkerLimit = 4 << 10
	jobNoiseLimit  = 8 << 10
	jobDiagLimit   = 600
)

type jobState int

const (
	jobSeekStart jobState = iota
	jobSeekPayload
	jobReadPayload
	jobSeekEnd
	jobDone
)

type jobCollector struct {
	runID string
	dst   io.Writer

	state   jobState
	buf     []byte
	remain  int64
	written int64

	base64 bool
	carry  []byte
	padded bool

	scan []byte

	ok     bool
	status string
	diag   []byte
	err    error
}

func newJobCollector(runID string, dst io.Writer) *jobCollector {
	return &jobCollector{runID: runID, dst: dst, state: jobSeekStart}
}

func (c *jobCollector) Write(p []byte) (int, error) {
	if c.err != nil {
		return len(p), nil
	}
	c.buf = append(c.buf, p...)
	for c.step() {
	}
	return len(p), nil
}

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

func (c *jobCollector) takeLine() (line string, ok bool) {
	if i := bytes.IndexByte(c.buf, '\n'); i >= 0 {
		line = string(c.buf[:i])
		c.buf = c.buf[i+1:]
		return line, true
	}
	if len(c.buf) > jobMarkerLimit {
		c.keepNoise(string(c.buf))
		c.buf = c.buf[:0]
	}
	return "", false
}

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

func (c *jobCollector) keepNoise(text string) {
	c.diag = append(c.diag, text...)
	c.diag = append(c.diag, '\n')
	if len(c.diag) > jobNoiseLimit {
		c.diag = c.diag[len(c.diag)-jobNoiseLimit:]
	}
}

func (c *jobCollector) Diagnostics() string {
	return strings.TrimSpace(string(c.diag))
}

func (c *jobCollector) Written() int64 { return c.written }

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

func boundedDiag(diag string) string {
	diag = strings.ReplaceAll(diag, "\x00", "")
	diag = strings.TrimSpace(diag)
	if len(diag) <= jobDiagLimit {
		return diag
	}
	return "…" + diag[len(diag)-jobDiagLimit:]
}
