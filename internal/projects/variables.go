package projects

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// variableKeyPattern is the contract's key rule: shell-style identifiers
// only. It is stricter than the application env-var rule on purpose, so a
// shared key is always a valid container variable without further quoting.
var variableKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// maxVariablesPerScope caps one PUT body per the API contract.
const maxVariablesPerScope = 128

// Variable is one shared KEY=VALUE pair as the API sees it. Secret values are
// write-only: Value is empty when Secret is true, so a secret's plaintext and
// ciphertext never leave the server in a response.
type Variable struct {
	Key    string
	Value  string
	Secret bool
}

// VariableInput is one row of a PUT body. A nil Value is an omitted value,
// which keeps an existing secret's sealed ciphertext instead of replacing it.
type VariableInput struct {
	Key    string
	Value  *string
	Secret bool
}

// SharedVariable is the stored row: plain pairs keep Value, secret pairs keep
// the AES-256-GCM ciphertext sealed with providers.SealSecret.
type SharedVariable struct {
	Key        string
	Value      string
	Ciphertext string
	Secret     bool
}

// GetProjectVariables returns one project's project-level variables (viewers
// may read). A foreign project answers ErrNotFound.
func (s *Service) GetProjectVariables(ctx context.Context, userID, projectID uuid.UUID) ([]Variable, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil || projectID == uuid.Nil {
		return nil, ErrNotFound
	}
	teamID := teamIDFor(ctx, userID)
	if _, err := s.repo.GetProject(ctx, teamID, projectID); err != nil {
		return nil, err
	}
	return s.listVariables(ctx, projectID, uuid.Nil)
}

// ReplaceProjectVariables replaces one project's whole project-level set and
// returns it masked (owner/admin). An omitted value for an existing secret
// key keeps its sealed ciphertext.
func (s *Service) ReplaceProjectVariables(ctx context.Context, userID, projectID uuid.UUID, inputs []VariableInput) ([]Variable, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil || projectID == uuid.Nil {
		return nil, ErrNotFound
	}
	if err := authorizeWrite(ctx, userID); err != nil {
		return nil, err
	}
	teamID := teamIDFor(ctx, userID)
	if _, err := s.repo.GetProject(ctx, teamID, projectID); err != nil {
		return nil, err
	}
	return s.replaceVariables(ctx, projectID, uuid.Nil, inputs)
}

// GetEnvironmentVariables returns one environment's variables (viewers may
// read). A foreign environment answers ErrNotFound.
func (s *Service) GetEnvironmentVariables(ctx context.Context, userID, environmentID uuid.UUID) ([]Variable, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil || environmentID == uuid.Nil {
		return nil, ErrNotFound
	}
	environment, err := s.repo.GetEnvironment(ctx, teamIDFor(ctx, userID), environmentID)
	if err != nil {
		return nil, err
	}
	return s.listVariables(ctx, environment.ProjectID, environment.ID)
}

// ReplaceEnvironmentVariables replaces one environment's whole set and
// returns it masked (owner/admin). An omitted value for an existing secret
// key keeps its sealed ciphertext.
func (s *Service) ReplaceEnvironmentVariables(ctx context.Context, userID, environmentID uuid.UUID, inputs []VariableInput) ([]Variable, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil || environmentID == uuid.Nil {
		return nil, ErrNotFound
	}
	if err := authorizeWrite(ctx, userID); err != nil {
		return nil, err
	}
	environment, err := s.repo.GetEnvironment(ctx, teamIDFor(ctx, userID), environmentID)
	if err != nil {
		return nil, err
	}
	return s.replaceVariables(ctx, environment.ProjectID, environment.ID, inputs)
}

// listVariables reads one scope and masks every secret.
func (s *Service) listVariables(ctx context.Context, projectID, environmentID uuid.UUID) ([]Variable, error) {
	rows, err := s.repo.ListVariables(ctx, projectID, environmentID)
	if err != nil {
		return nil, err
	}
	variables := make([]Variable, 0, len(rows))
	for _, row := range rows {
		variable := Variable{Key: row.Key, Secret: row.Secret}
		if !row.Secret {
			variable.Value = row.Value
		}
		variables = append(variables, variable)
	}
	return variables, nil
}

// replaceVariables validates a PUT body, seals new secret values and swaps
// the scope's whole set, returning the masked result. The validation runs
// before any write, so a bad body changes nothing.
func (s *Service) replaceVariables(ctx context.Context, projectID, environmentID uuid.UUID, inputs []VariableInput) ([]Variable, error) {
	if len(inputs) > maxVariablesPerScope {
		return nil, fmt.Errorf("%w: at most %d variables per scope", ErrValidation, maxVariablesPerScope)
	}
	existing, err := s.repo.ListVariables(ctx, projectID, environmentID)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]SharedVariable, len(existing))
	for _, row := range existing {
		byKey[row.Key] = row
	}
	vars := make([]SharedVariable, 0, len(inputs))
	seen := make(map[string]bool, len(inputs))
	for _, input := range inputs {
		if err := validateVariableKey(input.Key); err != nil {
			return nil, err
		}
		if seen[input.Key] {
			return nil, fmt.Errorf("%w: duplicate variable key %q", ErrValidation, input.Key)
		}
		seen[input.Key] = true
		if !input.Secret {
			value := ""
			if input.Value != nil {
				value = *input.Value
			}
			vars = append(vars, SharedVariable{Key: input.Key, Value: value})
			continue
		}
		if input.Value == nil {
			kept, ok := byKey[input.Key]
			if !ok || !kept.Secret {
				return nil, fmt.Errorf("%w: secret %q has no value", ErrValidation, input.Key)
			}
			vars = append(vars, SharedVariable{Key: kept.Key, Ciphertext: kept.Ciphertext, Secret: true})
			continue
		}
		ciphertext, err := providers.SealSecret(s.secret, *input.Value)
		if err != nil {
			return nil, fmt.Errorf("projects: seal shared variable %s: %w", input.Key, err)
		}
		vars = append(vars, SharedVariable{Key: input.Key, Ciphertext: ciphertext, Secret: true})
	}
	if err := s.repo.ReplaceVariables(ctx, projectID, environmentID, vars); err != nil {
		return nil, err
	}
	return s.listVariables(ctx, projectID, environmentID)
}

// validateVariableKey enforces the contract's key rule: ^[A-Za-z_][A-Za-z0-9_]*$.
func validateVariableKey(key string) error {
	if !variableKeyPattern.MatchString(key) {
		return fmt.Errorf("%w: variable key %q must match ^[A-Za-z_][A-Za-z0-9_]*$", ErrValidation, key)
	}
	return nil
}

// ListVariables returns one scope's stored rows (environmentID Nil reads the
// project level), ordered by key.
func (r *storeRepository) ListVariables(ctx context.Context, projectID, environmentID uuid.UUID) ([]SharedVariable, error) {
	rows, err := r.store.ListSharedVariables(ctx, pgUUID(projectID), pgUUID(environmentID))
	if err != nil {
		return nil, fmt.Errorf("projects: list shared variables: %w", err)
	}
	return sharedVariablesFromRows(rows), nil
}

// ReplaceVariables swaps one scope's whole set in a transaction.
func (r *storeRepository) ReplaceVariables(ctx context.Context, projectID, environmentID uuid.UUID, vars []SharedVariable) error {
	params := make([]sqlc.InsertSharedVariableParams, 0, len(vars))
	for _, variable := range vars {
		params = append(params, sqlc.InsertSharedVariableParams{
			ID:            pgUUID(uuid.New()),
			ProjectID:     pgUUID(projectID),
			EnvironmentID: pgUUID(environmentID),
			Key:           variable.Key,
			Value:         variable.Value,
			Ciphertext:    variable.Ciphertext,
			Secret:        variable.Secret,
		})
	}
	if err := r.store.ReplaceSharedVariables(ctx, pgUUID(projectID), pgUUID(environmentID), params); err != nil {
		return fmt.Errorf("projects: replace shared variables: %w", err)
	}
	return nil
}

// sharedVariablesFromRows maps sqlc rows to stored variables.
func sharedVariablesFromRows(rows []sqlc.SharedVariable) []SharedVariable {
	vars := make([]SharedVariable, 0, len(rows))
	for _, row := range rows {
		vars = append(vars, SharedVariable{
			Key:        row.Key,
			Value:      row.Value,
			Ciphertext: row.Ciphertext,
			Secret:     row.Secret,
		})
	}
	return vars
}

// variableResponse is the wire representation of one variable: value is
// omitted for secrets, so neither plaintext nor ciphertext ever leaves the
// server.
type variableResponse struct {
	Key    string  `json:"key"`
	Value  *string `json:"value,omitempty"`
	Secret bool    `json:"secret"`
}

// variablesEnvelope is the GET/PUT variables body.
type variablesEnvelope struct {
	Variables []variableResponse `json:"variables"`
}

// replaceVariablesRequest is the PUT variables body: it replaces the whole
// set, so an empty (or missing) list clears the scope.
type replaceVariablesRequest struct {
	Variables []variableInput `json:"variables"`
}

// variableInput is one row of a PUT body: a missing value keeps an existing
// secret's sealed ciphertext.
type variableInput struct {
	Key    string  `json:"key"`
	Value  *string `json:"value"`
	Secret bool    `json:"secret"`
}

// getProjectVariables serves GET /v1/projects/{id}/variables.
func (h *handler) getProjectVariables(w http.ResponseWriter, r *http.Request) {
	userID, projectID, ok := h.projectParams(w, r)
	if !ok {
		return
	}
	variables, err := h.svc.GetProjectVariables(r.Context(), userID, projectID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, variablesEnvelope{Variables: newVariableList(variables)})
}

// replaceProjectVariables serves PUT /v1/projects/{id}/variables: the body
// replaces the whole set and the masked set is returned.
func (h *handler) replaceProjectVariables(w http.ResponseWriter, r *http.Request) {
	userID, projectID, ok := h.projectParams(w, r)
	if !ok {
		return
	}
	var req replaceVariablesRequest
	if !decodeBody(w, r, &req) {
		return
	}
	variables, err := h.svc.ReplaceProjectVariables(r.Context(), userID, projectID, newVariableInputs(req.Variables))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, variablesEnvelope{Variables: newVariableList(variables)})
}

// getEnvironmentVariables serves GET /v1/environments/{id}/variables.
func (h *handler) getEnvironmentVariables(w http.ResponseWriter, r *http.Request) {
	userID, environmentID, ok := h.environmentParams(w, r)
	if !ok {
		return
	}
	variables, err := h.svc.GetEnvironmentVariables(r.Context(), userID, environmentID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, variablesEnvelope{Variables: newVariableList(variables)})
}

// replaceEnvironmentVariables serves PUT /v1/environments/{id}/variables:
// the body replaces the whole set and the masked set is returned.
func (h *handler) replaceEnvironmentVariables(w http.ResponseWriter, r *http.Request) {
	userID, environmentID, ok := h.environmentParams(w, r)
	if !ok {
		return
	}
	var req replaceVariablesRequest
	if !decodeBody(w, r, &req) {
		return
	}
	variables, err := h.svc.ReplaceEnvironmentVariables(r.Context(), userID, environmentID, newVariableInputs(req.Variables))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, variablesEnvelope{Variables: newVariableList(variables)})
}

// newVariableList maps domain variables to the wire, never rendering a null
// list and never a secret value.
func newVariableList(variables []Variable) []variableResponse {
	response := make([]variableResponse, 0, len(variables))
	for _, variable := range variables {
		row := variableResponse{Key: variable.Key, Secret: variable.Secret}
		if !variable.Secret {
			value := variable.Value
			row.Value = &value
		}
		response = append(response, row)
	}
	return response
}

// newVariableInputs maps wire rows to service inputs.
func newVariableInputs(inputs []variableInput) []VariableInput {
	converted := make([]VariableInput, 0, len(inputs))
	for _, input := range inputs {
		converted = append(converted, input.toInput())
	}
	return converted
}

// toInput maps one wire row to its service input (the shapes match field
// for field, so this is a plain conversion).
func (in variableInput) toInput() VariableInput {
	return VariableInput(in)
}

// sortedVariableKeys returns the sorted keys of a masked set (test helper for
// order-independent assertions stays with the mapping it asserts).
func sortedVariableKeys(variables []Variable) []string {
	keys := make([]string, 0, len(variables))
	for _, variable := range variables {
		keys = append(keys, variable.Key)
	}
	sort.Strings(keys)
	return keys
}
