package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/justindeelux/gotham/internal/providers"
)

const (
	headerGitHubSignature = "X-Hub-Signature-256"
	headerGitHubEvent     = "X-GitHub-Event"
	headerGitHubDelivery  = "X-GitHub-Delivery"

	headerGitLabToken    = "X-Gitlab-Token"
	headerGitLabEvent    = "X-Gitlab-Event"
	headerGitLabDelivery = "X-Gitlab-Event-UUID"

	headerGiteaSignature = "X-Gitea-Signature"
	headerGiteaEvent     = "X-Gitea-Event"
	headerGiteaDelivery  = "X-Gitea-Delivery"

	githubSignaturePrefix = "sha256="
)

const zeroCommit = "0000000000000000000000000000000000000000"

type signedDelivery struct {
	Event       string
	Repository  string
	Ref         string
	Commit      string
	DeliveryID  string
	Deleted     bool
	PullRequest *pullRequest
}

type pullRequest struct {
	Number     int
	Action     string
	HeadBranch string
	BaseBranch string
	HeadSHA    string
	Fork       bool
}

type pushPayload struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Checkout   string `json:"checkout_sha"`
	Repository *struct {
		FullName string `json:"full_name"`
		Path     string `json:"path"`
	} `json:"repository"`
	Project *struct {
		PathWithNamespace string `json:"path_with_namespace"`
		Path              string `json:"path"`
	} `json:"project"`
	HeadCommit *struct {
		ID string `json:"id"`
	} `json:"head_commit"`
}

type pullRequestPayload struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest *struct {
		Number int `json:"number"`
		Head   struct {
			Ref  string `json:"ref"`
			SHA  string `json:"sha"`
			Repo *struct {
				FullName string `json:"full_name"`
				Fork     bool   `json:"fork"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	} `json:"pull_request"`
	ObjectAttributes *struct {
		IID             int    `json:"iid"`
		Action          string `json:"action"`
		SourceBranch    string `json:"source_branch"`
		TargetBranch    string `json:"target_branch"`
		SourceProjectID int64  `json:"source_project_id"`
		TargetProjectID int64  `json:"target_project_id"`
		LastCommit      *struct {
			ID string `json:"id"`
		} `json:"last_commit"`
	} `json:"object_attributes"`
}

func (p pullRequestPayload) isForkHead(repo string) bool {
	if attrs := p.ObjectAttributes; attrs != nil && attrs.IID > 0 {
		return attrs.SourceProjectID == 0 || attrs.TargetProjectID == 0 ||
			attrs.SourceProjectID != attrs.TargetProjectID
	}
	if p.PullRequest == nil || p.PullRequest.Head.Repo == nil {
		return true
	}
	head := p.PullRequest.Head.Repo
	if head.Fork || head.FullName == "" {
		return true
	}
	return !strings.EqualFold(strings.TrimSpace(head.FullName), strings.TrimSpace(repo))
}

func (p pullRequestPayload) info(repo string) *pullRequest {
	if attrs := p.ObjectAttributes; attrs != nil && attrs.IID > 0 {
		pr := &pullRequest{
			Number:     attrs.IID,
			Action:     strings.ToLower(strings.TrimSpace(attrs.Action)),
			HeadBranch: strings.TrimSpace(attrs.SourceBranch),
			BaseBranch: strings.TrimSpace(attrs.TargetBranch),
			Fork:       p.isForkHead(repo),
		}
		if attrs.LastCommit != nil {
			pr.HeadSHA = strings.TrimSpace(attrs.LastCommit.ID)
		}
		return pr
	}
	if p.PullRequest == nil {
		return nil
	}
	number := p.PullRequest.Number
	if number == 0 {
		number = p.Number
	}
	if number <= 0 {
		return nil
	}
	return &pullRequest{
		Number:     number,
		Action:     strings.ToLower(strings.TrimSpace(p.Action)),
		HeadBranch: strings.TrimSpace(p.PullRequest.Head.Ref),
		BaseBranch: strings.TrimSpace(p.PullRequest.Base.Ref),
		HeadSHA:    strings.TrimSpace(p.PullRequest.Head.SHA),
		Fork:       p.isForkHead(repo),
	}
}

func parseDelivery(provider string, header http.Header, body []byte) (signedDelivery, error) {
	event, deliveryHeader := deliveryHeaders(provider)
	delivery := signedDelivery{
		Event:      strings.ToLower(strings.TrimSpace(header.Get(event))),
		DeliveryID: strings.TrimSpace(header.Get(deliveryHeader)),
	}

	var payload pushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return signedDelivery{}, fmt.Errorf("%w: body is not a JSON object", ErrBadRequest)
	}
	delivery.Ref = strings.TrimSpace(payload.Ref)
	switch {
	case payload.Repository != nil && payload.Repository.FullName != "":
		delivery.Repository = payload.Repository.FullName
	case payload.Repository != nil && payload.Repository.Path != "":
		delivery.Repository = payload.Repository.Path
	case payload.Project != nil && payload.Project.PathWithNamespace != "":
		delivery.Repository = payload.Project.PathWithNamespace
	case payload.Project != nil && payload.Project.Path != "":
		delivery.Repository = payload.Project.Path
	}
	if delivery.Repository == "" {
		return signedDelivery{}, fmt.Errorf("%w: delivery names no repository", ErrBadRequest)
	}

	var prPayload pullRequestPayload
	if err := json.Unmarshal(body, &prPayload); err == nil {
		delivery.PullRequest = prPayload.info(delivery.Repository)
	}

	delivery.Commit = strings.TrimSpace(payload.After)
	if delivery.Commit == zeroCommit {
		delivery.Deleted = true
		delivery.Commit = ""
	}
	if delivery.Commit == "" {
		delivery.Commit = strings.TrimSpace(payload.Checkout)
	}
	if delivery.Commit == zeroCommit {
		delivery.Deleted = true
		delivery.Commit = ""
	}
	if delivery.Commit == "" && payload.HeadCommit != nil {
		delivery.Commit = strings.TrimSpace(payload.HeadCommit.ID)
	}
	return delivery, nil
}

func deliveryHeaders(provider string) (event, delivery string) {
	switch provider {
	case providers.NameGitHub:
		return headerGitHubEvent, headerGitHubDelivery
	case providers.NameGitLab:
		return headerGitLabEvent, headerGitLabDelivery
	case providers.NameGitea:
		return headerGiteaEvent, headerGiteaDelivery
	default:
		return "", ""
	}
}

func verifySignature(provider string, header http.Header, body []byte, secret string) bool {
	if secret == "" {
		return false
	}
	switch provider {
	case providers.NameGitHub:
		provided := header.Get(headerGitHubSignature)
		if !strings.HasPrefix(provided, githubSignaturePrefix) {
			return false
		}
		provided = strings.TrimPrefix(provided, githubSignaturePrefix)
		return hmac.Equal([]byte(provided), []byte(hmacHex(secret, body)))
	case providers.NameGitLab:
		provided := header.Get(headerGitLabToken)
		return subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) == 1
	case providers.NameGitea:
		provided := header.Get(headerGiteaSignature)
		return hmac.Equal([]byte(provided), []byte(hmacHex(secret, body)))
	default:
		return false
	}
}

func isPushEvent(provider, event string) bool {
	switch provider {
	case providers.NameGitHub, providers.NameGitea:
		return event == "push"
	case providers.NameGitLab:
		return event == "push hook"
	default:
		return false
	}
}

func isPullRequestEvent(provider, event string) bool {
	switch provider {
	case providers.NameGitHub, providers.NameGitea:
		return event == "pull_request"
	case providers.NameGitLab:
		return event == "merge request hook"
	default:
		return false
	}
}

func branchOf(ref string) string {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "refs/heads/") {
		return ""
	}
	return strings.TrimPrefix(ref, "refs/heads/")
}

func hmacHex(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func readBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %v", ErrBadRequest, err)
	}
	if len(body) > maxBodyBytes {
		return nil, fmt.Errorf("%w: body exceeds %d bytes", ErrBadRequest, maxBodyBytes)
	}
	return body, nil
}
