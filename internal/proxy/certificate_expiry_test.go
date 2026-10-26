package proxy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type expirySource struct {
	certificates []DomainCertificate
	statuses     map[uuid.UUID]CertificateStatusObservation
	err          error
}

func (f *expirySource) ListCertificates(context.Context) ([]DomainCertificate, error) {
	return f.certificates, f.err
}

func (f *expirySource) CertificateStatuses(context.Context, []DomainCertificate) map[uuid.UUID]CertificateStatusObservation {
	return f.statuses
}

type expiryRecorder struct {
	events   []CertificateExpiry
	callback func(context.Context)
}

func (r *expiryRecorder) CertificateExpiring(ctx context.Context, event CertificateExpiry) {
	if r.callback != nil {
		r.callback(ctx)
	}
	r.events = append(r.events, event)
}

func TestCertificateExpiryObservations(t *testing.T) {
	type observationStep struct {
		status    CertificateStatus
		expiry    time.Time
		disabled  bool
		readError bool
		advance   time.Duration
		want      int
	}
	start := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	near := start.Add(7 * 24 * time.Hour)
	renewed := near.Add(90 * 24 * time.Hour)
	for _, tc := range []struct {
		name  string
		steps []observationStep
	}{
		{name: "outside threshold", steps: []observationStep{
			{status: CertificateStatusPresent, expiry: near.Add(3 * time.Hour)},
			{status: CertificateStatusPresent, expiry: near.Add(3 * time.Hour)},
		}},
		{name: "exact threshold and no repeats", steps: []observationStep{
			{status: CertificateStatusPresent, expiry: near},
			{status: CertificateStatusPresent, expiry: near, want: 1},
			{status: CertificateStatusPresent, expiry: near, want: 1},
		}},
		{name: "unknown absent disabled and zero expiry skipped", steps: []observationStep{
			{status: CertificateStatusUnknown, expiry: near},
			{status: CertificateStatusAbsent, expiry: near},
			{status: CertificateStatusPresent, expiry: near, disabled: true},
			{status: CertificateStatusPresent},
			{status: CertificateStatusPresent, expiry: near},
			{status: CertificateStatusPresent, expiry: near, want: 1},
		}},
		{name: "errors and unknown do not reset", steps: []observationStep{
			{status: CertificateStatusPresent, expiry: near},
			{readError: true},
			{status: CertificateStatusUnknown},
			{status: CertificateStatusPresent, expiry: near, want: 1},
		}},
		{name: "renewal rearms", steps: []observationStep{
			{status: CertificateStatusPresent, expiry: near},
			{status: CertificateStatusPresent, expiry: near, want: 1},
			{status: CertificateStatusPresent, expiry: renewed, want: 1},
			{status: CertificateStatusPresent, expiry: renewed, advance: 90 * 24 * time.Hour, want: 1},
			{status: CertificateStatusPresent, expiry: renewed, want: 2},
			{status: CertificateStatusPresent, expiry: renewed, want: 2},
			{status: CertificateStatusPresent, expiry: near, want: 2},
			{status: CertificateStatusPresent, expiry: near, want: 2},
		}},
		{name: "expired certificate qualifies", steps: []observationStep{
			{status: CertificateStatusPresent, expiry: start.Add(-time.Hour)},
			{status: CertificateStatusPresent, expiry: start.Add(-time.Hour), want: 1},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			source := &expirySource{certificates: []DomainCertificate{{ID: id, Domain: "panel.example.com", Enabled: true}}}
			recorder := &expiryRecorder{}
			observer := NewCertificateExpiryObserver(CertificateExpiryConfig{Store: source, Status: source, Notifier: recorder})
			now := start
			observer.now = func() time.Time { return now }
			for i, step := range tc.steps {
				now = now.Add(step.advance)
				source.certificates[0].Enabled = !step.disabled
				source.statuses = map[uuid.UUID]CertificateStatusObservation{id: {Status: step.status, NotAfter: step.expiry}}
				source.err = nil
				if step.readError {
					source.err = errors.New("read failed")
				}
				observer.observe(context.Background())
				if len(recorder.events) != step.want {
					t.Fatalf("step %d: got %d events, want %d", i, len(recorder.events), step.want)
				}
				now = now.Add(time.Hour)
			}
			for _, event := range recorder.events {
				if event.Domain != "panel.example.com" || event.NotAfter.IsZero() || event.At.IsZero() {
					t.Fatalf("event: %+v", event)
				}
			}
		})
	}
}

func TestCertificateExpiryIndependentCertificates(t *testing.T) {
	now := time.Now()
	first, second := uuid.New(), uuid.New()
	source := &expirySource{
		certificates: []DomainCertificate{{ID: first, Domain: "panel.example.com", Enabled: true}, {ID: second, Domain: "app.example.com", ApplicationID: uuid.New(), Enabled: true}},
		statuses:     map[uuid.UUID]CertificateStatusObservation{first: {Status: CertificateStatusPresent, NotAfter: now.Add(24 * time.Hour)}, second: {Status: CertificateStatusUnknown}},
	}
	recorder := &expiryRecorder{}
	observer := NewCertificateExpiryObserver(CertificateExpiryConfig{Store: source, Status: source, Notifier: recorder})
	observer.now = func() time.Time { return now }
	observer.observe(context.Background())
	source.statuses[second] = CertificateStatusObservation{Status: CertificateStatusPresent, NotAfter: now.Add(24 * time.Hour)}
	observer.observe(context.Background())
	if len(recorder.events) != 1 || recorder.events[0].Domain != "panel.example.com" {
		t.Fatalf("events: %+v", recorder.events)
	}
	observer.observe(context.Background())
	if len(recorder.events) != 2 || recorder.events[1].Domain != "app.example.com" {
		t.Fatalf("events: %+v", recorder.events)
	}
}

func TestCertificateExpiryLoopAndShutdown(t *testing.T) {
	now := time.Now()
	id := uuid.New()
	source := &expirySource{
		certificates: []DomainCertificate{{ID: id, Domain: "panel.example.com", Enabled: true}},
		statuses:     map[uuid.UUID]CertificateStatusObservation{id: {Status: CertificateStatusPresent, NotAfter: now.Add(time.Hour)}},
	}
	notified := make(chan struct{}, 1)
	recorder := &expiryRecorder{callback: func(ctx context.Context) {
		deadline, ok := ctx.Deadline()
		if ctx.Err() != nil || !ok || time.Until(deadline) > 5*time.Second {
			t.Error("notification needs an active, bounded context")
		}
		notified <- struct{}{}
	}}
	observer := NewCertificateExpiryObserver(CertificateExpiryConfig{Store: source, Status: source, Notifier: recorder})
	observer.now = func() time.Time { return now }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() { defer close(done); observer.run(ctx, ticks) }()
	select {
	case ticks <- now.Add(time.Hour):
	case <-time.After(5 * time.Second):
		t.Fatal("observer did not wait for its hourly tick")
	}
	select {
	case <-notified:
	case <-time.After(5 * time.Second):
		t.Fatal("second observation did not notify")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("observer did not stop on shutdown")
	}
	if len(recorder.events) != 1 {
		t.Fatalf("events: %+v", recorder.events)
	}
}
