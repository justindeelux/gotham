package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNotificationFramesReachOwnersAndProjectMembers(t *testing.T) {
	repo := newFakeRepository()
	pub := newFakePublisher()
	service := newTestService(t, repo, pub)
	owner, member := uuid.New(), uuid.New()
	event := sampleEvent()
	projectID := uuid.New()
	repo.resourceProjects[event.ResourceID] = projectID
	repo.notificationRecipients[projectID] = []uuid.UUID{owner, member}

	service.Dispatch(event)

	waitFor(t, "the notification frames", func() bool {
		return len(pub.channels()) == 2
	})
	want := []string{UserChannel(owner), UserChannel(member)}
	channels := pub.channels()
	if len(channels) != len(want) {
		t.Fatalf("channels = %v, want %v", channels, want)
	}
	seen := make(map[string]bool, len(channels))
	for _, channel := range channels {
		seen[channel] = true
	}
	for _, channel := range want {
		if !seen[channel] {
			t.Fatalf("channels = %v, want %v", channels, want)
		}
	}
	page, err := service.ListNotifications(context.Background(), owner, 10)
	if err != nil || len(page.Notifications) != 1 {
		t.Fatalf("list notifications = %+v, %v", page, err)
	}
	wantItem, err := json.Marshal(page.Notifications[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, channel := range want {
		frames := pub.framesFor(channel)
		if len(frames) != 1 {
			t.Fatalf("frames on %s = %d, want 1", channel, len(frames))
		}
		if frames[0].Type != "notification" || frames[0].Channel != channel {
			t.Errorf("frame = %+v, want a notification frame on %s", frames[0], channel)
		}
		if frames[0].Data != string(wantItem) {
			t.Errorf("data = %s, want the list item %s", frames[0].Data, wantItem)
		}
	}
}

func TestNotificationFramesStayOwnerOnlyWithoutProject(t *testing.T) {
	repo := newFakeRepository()
	pub := newFakePublisher()
	service := newTestService(t, repo, pub)
	owner, member := uuid.New(), uuid.New()
	repo.notificationRecipients[uuid.Nil] = []uuid.UUID{owner}

	service.Dispatch(sampleEvent())

	waitFor(t, "the owner notification frame", func() bool {
		return len(pub.framesFor(UserChannel(owner))) == 1
	})
	if frames := pub.framesFor(UserChannel(member)); len(frames) != 0 {
		t.Fatalf("member frames = %d, want none", len(frames))
	}
}

func TestRevokedMemberGetsNoFrameForNextNotification(t *testing.T) {
	repo := newFakeRepository()
	pub := newFakePublisher()
	service := newTestService(t, repo, pub)
	owner, member := uuid.New(), uuid.New()
	projectID := uuid.New()
	repo.notificationRecipients[projectID] = []uuid.UUID{owner, member}
	first := sampleEvent()
	repo.resourceProjects[first.ResourceID] = projectID

	service.Dispatch(first)
	waitFor(t, "the first member frame", func() bool {
		return len(pub.framesFor(UserChannel(member))) == 1
	})

	repo.notificationRecipients[projectID] = []uuid.UUID{owner}
	second := sampleEvent()
	second.At = first.At.Add(time.Minute)
	repo.resourceProjects[second.ResourceID] = projectID
	service.Dispatch(second)
	waitFor(t, "the second owner frame", func() bool {
		return len(pub.framesFor(UserChannel(owner))) == 2
	})
	if frames := pub.framesFor(UserChannel(member)); len(frames) != 1 {
		t.Fatalf("revoked member frames = %d, want the first frame only", len(frames))
	}
}

func TestPublishFailureKeepsInsertAndDelivery(t *testing.T) {
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	repo.notificationRecipients[uuid.Nil] = []uuid.UUID{uuid.New()}
	pub := newFakePublisher()
	pub.err = errors.New("publish failed")
	service := newTestService(t, repo, pub)
	var logs syncBuffer
	service.logger = slog.New(slog.NewTextHandler(&logs, nil))
	notifier := newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: notifier})

	service.Dispatch(sampleEvent())

	waitFor(t, "the channel delivery", func() bool {
		return len(notifier.delivered()) == 1
	})
	if rows := repo.notificationsSnapshot(); len(rows) != 1 {
		t.Fatalf("inbox rows = %d, want the insert to survive", len(rows))
	}
	waitFor(t, "the publish failure log", func() bool {
		return strings.Contains(logs.String(), "publish realtime notification")
	})
}
