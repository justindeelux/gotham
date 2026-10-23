package deploy

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (s *Service) GetDeployment(ctx context.Context, userID, appID, deploymentID uuid.UUID) (Deployment, error) {
	if _, err := s.application(ctx, userID, appID, false); err != nil {
		return Deployment{}, err
	}
	if deploymentID == uuid.Nil {
		return Deployment{}, fmt.Errorf("%w: invalid deployment id", ErrValidation)
	}
	return s.repo.GetDeployment(ctx, appID, deploymentID)
}

func (h *handler) getDeployment(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := applicationIDParam(w, r)
	if !ok {
		return
	}
	deploymentID, err := uuid.Parse(chi.URLParam(r, "deployment_id"))
	if err != nil || deploymentID == uuid.Nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid deployment id"})
		return
	}
	deployment, err := h.svc.GetDeployment(r.Context(), userID, appID, deploymentID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, deploymentEnvelope{Deployment: newDeploymentResponse(deployment)})
}
