package deploy

import (
	"context"
	"time"

	"github.com/google/uuid"
)

const notifyTimeout = 5 * time.Second

type DeployResult struct {
	DeploymentID  uuid.UUID
	ApplicationID uuid.UUID
	Application   string
	Kind          Kind
	State         State
	Error         string
	Host          string
	StartedAt     time.Time
	FinishedAt    time.Time
}

type Notifier interface {
	DeployFinished(ctx context.Context, result DeployResult)
}

func (o *Orchestrator) notify(ctx context.Context, st *runState, to State) {
	if o.notifier == nil || (to != StateRunning && to != StateFailed) {
		return
	}
	notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), notifyTimeout)
	defer cancel()
	host := ""
	if st.app.PrimaryDomain != nil {
		host = st.app.PrimaryDomain.Domain
	}
	o.notifier.DeployFinished(notifyCtx, DeployResult{
		DeploymentID:  st.dep.ID,
		ApplicationID: st.app.ID,
		Application:   st.app.Name,
		Kind:          st.dep.Kind,
		State:         to,
		Error:         st.dep.Error,
		Host:          host,
		StartedAt:     st.dep.StartedAt,
		FinishedAt:    st.dep.FinishedAt,
	})
}
