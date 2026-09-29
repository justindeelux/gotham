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

// Delivery headers, one set per Git host.
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

	// githubSignaturePrefix heads the hex digest GitHub sends.
	githubSignaturePrefix = "sha256="
)

// zeroCommit is the SHA Git hosts send when a ref was deleted.
const zeroCommit = "0000000000000000000000000000000000000000"

// signedDelivery is the untrusted routing information of one delivery: exactly
// what is needed to find the secret that should verify it, and nothing else.
// It is read before the signature is checked, so no field of it may be acted
// on until verifySignature has accepted the body.
type signedDelivery struct {
	Event      string
	Repository string
	Ref        string
	Commit     string
	DeliveryID string
	// Deleted marks the zero commit Git hosts send when the ref was removed:
	// there is nothing left to build.
	Deleted bool
	// PullRequest is set when the delivery is a pull_request (GitLab: merge
	// request) notification. It is routing data read before the signature is
	// checked, so it must not be acted on until verifySignature accepts the
	// body. nil for every other event.
	PullRequest *pullRequest
}

// pullRequest is the routing information of one pull request delivery: which
// PR, what happened, the head/base branches the preview decision needs, and
// whether the head comes from a fork (a fork PR must not be previewed: its
// code is not the repository the base application trusts).
type pullRequest struct {
	Number     int
	Action     string
	HeadBranch string
	BaseBranch string
	HeadSHA    string
	Fork       bool
}

// pushPayload is the subset of a push body shared by GitHub, GitLab and Gitea.
// Field names overlap enough that one tolerant struct reads all three; missing
// fields stay empty.
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

// pullRequestPayload is the subset of a pull_request (GitLab: merge request)
// body shared by GitHub, GitLab and Gitea. Missing fields stay empty, so one
// tolerant struct reads all three; pullRequestPayload.info decides whether the
// body actually is a pull request delivery.
type pullRequestPayload struct {
	Action string `json:"action"`
	Number int    `json:"number"`
	// GitHub and Gitea nest the pull request under pull_request; GitLab names
	// the merge request's fields directly under object_attributes.
	PullRequest *struct {
		Number int `json:"number"`
		Head   struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
			// Repo is the head repository. GitHub and Gitea set fork=true (and
			// a foreign full_name) for a pull request opened from a fork; a
			// missing object cannot be proven a fork and is treated as
			// same-repository only when full_name is absent too.
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
		IID          int    `json:"iid"`
		Action       string `json:"action"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		// GitLab: an MR from a fork has a source project different from the
		// target project.
		SourceProjectID int64 `json:"source_project_id"`
		TargetProjectID int64 `json:"target_project_id"`
		LastCommit      *struct {
			ID string `json:"id"`
		} `json:"last_commit"`
	} `json:"object_attributes"`
}

// isForkHead reports whether a pull request's head comes from another
// repository (or repository fork). repo is the delivery's repository
// identifier ("owner/name" / "group/project").
func (p pullRequestPayload) isForkHead(repo string) bool {
	if attrs := p.ObjectAttributes; attrs != nil && attrs.IID > 0 {
		return attrs.SourceProjectID != 0 && attrs.TargetProjectID != 0 &&
			attrs.SourceProjectID != attrs.TargetProjectID
	}
	if p.PullRequest == nil || p.PullRequest.Head.Repo == nil {
		return false
	}
	head := p.PullRequest.Head.Repo
	if head.Fork {
		return true
	}
	// A head.repository naming a different repository is a fork even when the
	// host did not set the flag.
	return head.FullName != "" && !strings.EqualFold(strings.TrimSpace(head.FullName), strings.TrimSpace(repo))
}

// info turns a parsed body into the pull request routing facts, or nil when
// the body carries no pull request. A missing or non-positive number means the
// delivery cannot address a preview and is reported as "not a pull request".
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

// parseDelivery reads the routing facts of a delivery from its headers and
// body. It never validates the signature — that is verifySignature's job — and
// fails only when the body is not JSON at all or names no repository.
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

	// The push and pull_request shapes overlap only in repository/project, so
	// the pull request facts are decoded from the same bounded body after the
	// repository is known (fork detection compares the head repository with
	// it). A push body leaves PullRequest nil.
	var prPayload pullRequestPayload
	if err := json.Unmarshal(body, &prPayload); err == nil {
		delivery.PullRequest = prPayload.info(delivery.Repository)
	}

	delivery.Commit = strings.TrimSpace(payload.After)
	if delivery.Commit == zeroCommit {
		delivery.Deleted = true // a deleted ref carries no commit to build
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

// deliveryHeaders returns the event and delivery-ID header names a provider
// uses. The signature header is deliberately not returned: verification reads
// it directly so the mapping lives next to the digest rules.
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

// verifySignature reports whether body authenticates as a delivery from
// provider signed with secret. Every comparison is constant time: the shared
// token is compared with subtle.ConstantTimeCompare and the HMAC digests with
// hmac.Equal, so a delivery cannot be probed byte by byte.
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

// isPushEvent reports whether a verified delivery asks for a branch build.
// Tag pushes and every other event are acknowledged without deploying.
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

// isPullRequestEvent reports whether a verified delivery asks for preview
// handling. GitLab calls the event "Merge Request Hook"; GitHub and Gitea use
// "pull_request".
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

// branchOf turns a git ref into the branch name it addresses, or "" when the
// ref is not a branch (tags, in particular, must not start a build).
func branchOf(ref string) string {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "refs/heads/") {
		return ""
	}
	return strings.TrimPrefix(ref, "refs/heads/")
}

// hmacHex returns hex(HMAC-SHA256(secret, body)), the digest shape GitHub and
// Gitea sign with.
func hmacHex(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body) // hash.Hash never fails
	return hex.EncodeToString(mac.Sum(nil))
}

// readBody reads a bounded delivery body. Bodies larger than maxBodyBytes are
// refused instead of truncated: a truncated body would fail its signature
// anyway, and refusing keeps the memory bound honest.
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
