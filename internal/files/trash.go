package files

import (
	"context"
	"strings"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type RestoreRequest struct {
	Root        string `json:"root"`
	ID          string `json:"id"`
	Destination string `json:"destination"`
	Overwrite   bool   `json:"overwrite"`
}

type PurgeRequest struct {
	Root string `json:"root"`
	ID   string `json:"id"`
}

type EmptyTrashRequest struct {
	Root string `json:"root"`
}

type TrashListing struct {
	Entries   []TrashEntry `json:"entries"`
	Truncated bool         `json:"truncated"`
}

func (s *Service) Trash(ctx context.Context, serverID uuid.UUID, root string) (listing TrashListing, err error) {
	defer func() { s.audit(ctx, serverID, "trash_list", root, "", err) }()
	if !Enabled() {
		return TrashListing{}, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return TrashListing{}, err
	}
	ctx, client, cancel, err := s.rpc(ctx, serverID, false)
	if err != nil {
		return TrashListing{}, err
	}
	defer cancel()
	defer s.closeClient(client)

	response, err := client.ListTrash(ctx, &agentv1.ListTrashRequest{Root: root})
	if err != nil {
		return TrashListing{}, mapRPCError(err)
	}
	entries := make([]TrashEntry, 0, len(response.GetEntries()))
	for _, info := range response.GetEntries() {
		entries = append(entries, newTrashEntry(info))
	}
	return TrashListing{Entries: entries, Truncated: response.GetTruncated()}, nil
}

func (s *Service) Restore(ctx context.Context, serverID uuid.UUID, root, id, destination string, overwrite bool) (entry Entry, err error) {
	restoredPath := destination
	defer func() {
		s.audit(ctx, serverID, "trash_restore", root, restoredPath, err, "id", id, "overwrite", overwrite)
	}()
	if !Enabled() {
		return Entry{}, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return Entry{}, err
	}
	if err := validatePathArg("id", id); err != nil {
		return Entry{}, err
	}
	if strings.TrimSpace(destination) != "" {
		if err := validatePathArg("destination", destination); err != nil {
			return Entry{}, err
		}
	}

	ctx, client, cancel, err := s.rpcTransfer(ctx, serverID)
	if err != nil {
		return Entry{}, err
	}
	defer cancel()
	defer s.closeClient(client)

	response, err := client.Restore(ctx, &agentv1.RestoreRequest{
		Root:        root,
		Id:          id,
		Destination: destination,
		Overwrite:   overwrite,
	})
	if err != nil {
		return Entry{}, mapRPCError(err)
	}
	if path := response.GetPath(); path != "" {
		restoredPath = path
	}
	info := response.GetEntry()
	if info == nil {
		return Entry{}, ErrNotFound
	}
	return newEntry(info), nil
}

func (s *Service) Purge(ctx context.Context, serverID uuid.UUID, root, id string) (err error) {
	defer func() { s.audit(ctx, serverID, "trash_purge", root, id, err) }()
	if !Enabled() {
		return ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return err
	}
	if err := validatePathArg("id", id); err != nil {
		return err
	}

	ctx, client, cancel, err := s.rpcTransfer(ctx, serverID)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.closeClient(client)

	if _, err := client.Purge(ctx, &agentv1.PurgeRequest{Root: root, Id: id}); err != nil {
		return mapRPCError(err)
	}
	return nil
}

func (s *Service) EmptyTrash(ctx context.Context, serverID uuid.UUID, root string) (removed uint64, err error) {
	defer func() { s.audit(ctx, serverID, "trash_empty", root, "", err) }()
	if !Enabled() {
		return 0, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return 0, err
	}

	ctx, client, cancel, err := s.rpcTransfer(ctx, serverID)
	if err != nil {
		return 0, err
	}
	defer cancel()
	defer s.closeClient(client)

	response, err := client.EmptyTrash(ctx, &agentv1.EmptyTrashRequest{Root: root})
	if err != nil {
		return 0, mapRPCError(err)
	}
	return response.GetRemoved(), nil
}

func newTrashEntry(info *agentv1.TrashEntry) TrashEntry {
	entry := TrashEntry{
		ID:           info.GetId(),
		Name:         info.GetName(),
		OriginalPath: info.GetOriginalPath(),
		Size:         info.GetSize(),
		Kind:         kindString(info.GetKind()),
	}
	if ts := info.GetDeletedAt(); ts != nil {
		entry.DeletedAt = ts.AsTime()
	}
	return entry
}
