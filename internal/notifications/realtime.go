package notifications

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

const notificationFrameType = "notification"

type Publisher interface {
	Publish(ctx context.Context, channel, payload string) error
}

func UserChannel(userID uuid.UUID) string { return "notifications:" + userID.String() }

type Frame struct {
	Channel string `json:"channel"`
	Type    string `json:"type"`
	Data    string `json:"data"`
}

type Emitter struct{ pub Publisher }

func NewEmitter(pub Publisher) *Emitter { return &Emitter{pub: pub} }

func (e *Emitter) Emit(ctx context.Context, userID uuid.UUID, view NotificationView) error {
	if e == nil || e.pub == nil {
		return nil
	}
	data, err := json.Marshal(view)
	if err != nil {
		return err
	}
	channel := UserChannel(userID)
	frame, err := json.Marshal(Frame{Channel: channel, Type: notificationFrameType, Data: string(data)})
	if err != nil {
		return err
	}
	return e.pub.Publish(ctx, channel, string(frame))
}
