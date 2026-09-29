package deploy

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// notifyTimeout bounds how long the terminal hook may take. The deployment
// row is already durable when it runs, so a slow consumer is cut off instead
// of delaying the worker.
const notifyTimeout = 5 * time.Second

// DeployResult is the terminal outcome of one deployment, handed to the
// notification hook. It carries no secrets: only names, outcome and the
// bounded error text.
type DeployResult struct {
	DeploymentID  uuid.UUID
	ApplicationID uuid.UUID
	Application   string
	TeamID        uuid.UUID
	Kind          Kind
	State         State
	Error         string
	// Host is the application's base domain; empty when it has none.
	Host       string
	StartedAt  time.Time
	FinishedAt time.Time
}

// Notifier receives one terminal deployment result (running or failed). It
// must not block the deploy path; internal/notifications queues the delivery
// on a bounded worker pool. A nil Notifier disables notifications.
type Notifier interface {
	DeployFinished(ctx context.Context, result DeployResult)
}

// notify hands the terminal result to the configured notifier, best effort:
// the deploy state is already persisted and a notification can never fail a
// deployment. The context is detached from the run (a failed deploy often
// runs on a cancelled context) but bounded by notifyTimeout.
func (o *Orchestrator) notify(ctx context.Context, st *runState, to State) {
	if o.notifier == nil || (to != StateRunning && to != StateFailed) {
		return
	}
	notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), notifyTimeout)
	defer cancel()
	o.notifier.DeployFinished(notifyCtx, DeployResult{
		DeploymentID:  st.dep.ID,
		ApplicationID: st.app.ID,
		Application:   st.app.Name,
		TeamID:        st.app.TeamID,
		Kind:          st.dep.Kind,
		State:         to,
		Error:         st.dep.Error,
		Host:          st.app.BaseDomain,
		StartedAt:     st.dep.StartedAt,
		FinishedAt:    st.dep.FinishedAt,
	})
}
