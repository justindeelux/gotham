package terminal

import "time"

const (
	Subprotocol         = "gotham.terminal.v1"
	TokenProtocolPrefix = "gotham.token."

	opInput     byte = 0x01
	opResize    byte = 0x02
	opClose     byte = 0x03
	opHeartbeat byte = 0x04
	opOutput    byte = 0x01
	opExited    byte = 0x02
	opError     byte = 0x03
	opOpened    byte = 0x04
	opPing      byte = 0x05
)

const (
	maxInputBytes = 64 << 10
	maxWSFrame    = maxInputBytes + 1
	maxErrorBytes = 512
	openTimeout   = 20 * time.Second
)

type openedEvent struct {
	Shell         string `json:"shell"`
	Cols          uint32 `json:"cols"`
	Rows          uint32 `json:"rows"`
	ServiceUser   string `json:"service_user"`
	Target        string `json:"target,omitempty"`
	Service       string `json:"service,omitempty"`
	ContainerID   string `json:"container_id,omitempty"`
	ContainerName string `json:"container_name,omitempty"`
	TargetLabel   string `json:"target_label,omitempty"`
}

type exitedEvent struct {
	ExitCode int32  `json:"exit_code"`
	Signal   string `json:"signal"`
}
