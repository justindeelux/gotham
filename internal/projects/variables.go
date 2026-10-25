package projects

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

var variableKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

const maxVariablesPerScope = 128

const maxVariableKeyLength = 128

type Variable struct {
	Key    string
	Value  string
	Secret bool
}

type VariableInput struct {
	Key    string
	Value  *string
	Secret bool
}

type SharedVariable struct {
	Key        string
	Value      string
	Ciphertext string
	Secret     bool
}

func (s *Service) GetProjectVariables(ctx context.Context, userID, projectID uuid.UUID) ([]Variable, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil || projectID == uuid.Nil {
		return nil, ErrNotFound
	}
	if _, err := s.repo.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.listVariables(ctx, projectID, uuid.Nil)
}

func (s *Service) ReplaceProjectVariables(ctx context.Context, userID, projectID uuid.UUID, inputs []VariableInput) ([]Variable, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil || projectID == uuid.Nil {
		return nil, ErrNotFound
	}
	if _, err := s.repo.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.replaceVariables(ctx, projectID, uuid.Nil, inputs)
}

func (s *Service) GetEnvironmentVariables(ctx context.Context, userID, environmentID uuid.UUID) ([]Variable, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil || environmentID == uuid.Nil {
		return nil, ErrNotFound
	}
	environment, err := s.repo.GetEnvironment(ctx, environmentID)
	if err != nil {
		return nil, err
	}
	return s.listVariables(ctx, environment.ProjectID, environment.ID)
}

func (s *Service) ReplaceEnvironmentVariables(ctx context.Context, userID, environmentID uuid.UUID, inputs []VariableInput) ([]Variable, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil || environmentID == uuid.Nil {
		return nil, ErrNotFound
	}
	environment, err := s.repo.GetEnvironment(ctx, environmentID)
	if err != nil {
		return nil, err
	}
	return s.replaceVariables(ctx, environment.ProjectID, environment.ID, inputs)
}

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

func (s *Service) replaceVariables(ctx context.Context, projectID, environmentID uuid.UUID, inputs []VariableInput) ([]Variable, error) {
	if len(inputs) > maxVariablesPerScope {
		return nil, fmt.Errorf("%w: at most %d variables per scope", ErrValidation, maxVariablesPerScope)
	}
	err := s.repo.ReplaceVariables(ctx, projectID, environmentID, func(existing []SharedVariable) ([]SharedVariable, error) {
		return s.buildVariableSet(existing, inputs)
	})
	if err != nil {
		return nil, err
	}
	return s.listVariables(ctx, projectID, environmentID)
}

func (s *Service) buildVariableSet(existing []SharedVariable, inputs []VariableInput) ([]SharedVariable, error) {
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
			if err := validateVariableValue(input.Key, value); err != nil {
				return nil, err
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
		if err := validateVariableValue(input.Key, *input.Value); err != nil {
			return nil, err
		}
		ciphertext, err := providers.SealSecret(s.secret, *input.Value)
		if err != nil {
			return nil, fmt.Errorf("projects: seal shared variable %s: %w", input.Key, err)
		}
		vars = append(vars, SharedVariable{Key: input.Key, Ciphertext: ciphertext, Secret: true})
	}
	return vars, nil
}

func validateVariableKey(key string) error {
	if !variableKeyPattern.MatchString(key) {
		return fmt.Errorf("%w: variable key %q must match ^[A-Za-z_][A-Za-z0-9_]*$", ErrValidation, key)
	}
	if len(key) > maxVariableKeyLength {
		return fmt.Errorf("%w: variable key %q must be at most %d characters", ErrValidation, key, maxVariableKeyLength)
	}
	return nil
}

func validateVariableValue(key, value string) error {
	if strings.ContainsRune(value, 0) {
		return fmt.Errorf("%w: variable %q value must not contain NUL", ErrValidation, key)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%w: variable %q value must be valid UTF-8", ErrValidation, key)
	}
	return nil
}

func (r *storeRepository) ListVariables(ctx context.Context, projectID, environmentID uuid.UUID) ([]SharedVariable, error) {
	rows, err := r.store.ListSharedVariables(ctx, pgUUID(projectID), pgUUID(environmentID))
	if err != nil {
		return nil, fmt.Errorf("projects: list shared variables: %w", err)
	}
	return sharedVariablesFromRows(rows), nil
}

func (r *storeRepository) ReplaceVariables(ctx context.Context, projectID, environmentID uuid.UUID, build func(existing []SharedVariable) ([]SharedVariable, error)) error {
	err := r.store.ReplaceSharedVariablesLocked(ctx, pgUUID(projectID), pgUUID(environmentID),
		func(existing []sqlc.SharedVariable) ([]sqlc.InsertSharedVariableParams, error) {
			vars, err := build(sharedVariablesFromRows(existing))
			if err != nil {
				return nil, err
			}
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
			return params, nil
		})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if isForeignKeyViolation(err) {
			return ErrNotFound
		}
		return fmt.Errorf("projects: replace shared variables: %w", err)
	}
	return nil
}

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

type variableResponse struct {
	Key    string  `json:"key"`
	Value  *string `json:"value,omitempty"`
	Secret bool    `json:"secret"`
}

type variablesEnvelope struct {
	Variables []variableResponse `json:"variables"`
}

type replaceVariablesRequest struct {
	Variables []variableInput `json:"variables"`
}

type variableInput struct {
	Key    string  `json:"key"`
	Value  *string `json:"value"`
	Secret bool    `json:"secret"`
}

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

func newVariableInputs(inputs []variableInput) []VariableInput {
	converted := make([]VariableInput, 0, len(inputs))
	for _, input := range inputs {
		converted = append(converted, input.toInput())
	}
	return converted
}

func (in variableInput) toInput() VariableInput {
	return VariableInput(in)
}

func sortedVariableKeys(variables []Variable) []string {
	keys := make([]string, 0, len(variables))
	for _, variable := range variables {
		keys = append(keys, variable.Key)
	}
	sort.Strings(keys)
	return keys
}
