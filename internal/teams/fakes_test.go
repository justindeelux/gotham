package teams

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

type fakeRepository struct {
	mu             sync.Mutex
	teams          map[uuid.UUID]Team
	members        map[uuid.UUID]map[uuid.UUID]Member
	invites        map[uuid.UUID]storedInvite
	users          map[uuid.UUID]string
	resourceCounts map[uuid.UUID]int64
}

type storedInvite struct {
	invite Invite
	hash   string
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		teams:          map[uuid.UUID]Team{},
		members:        map[uuid.UUID]map[uuid.UUID]Member{},
		invites:        map[uuid.UUID]storedInvite{},
		users:          map[uuid.UUID]string{},
		resourceCounts: map[uuid.UUID]int64{},
	}
}

func (f *fakeRepository) seedResourceCount(teamID uuid.UUID, count int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resourceCounts[teamID] = count
}

func (f *fakeRepository) seedUser(userID uuid.UUID, email string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users[userID] = email
}

func (f *fakeRepository) seedTeam(team Team, ownerID uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if team.CreatedAt.IsZero() {
		team.CreatedAt = time.Now().UTC()
	}
	team.UpdatedAt = team.CreatedAt
	f.teams[team.ID] = team
	if f.members[team.ID] == nil {
		f.members[team.ID] = map[uuid.UUID]Member{}
	}
	f.members[team.ID][ownerID] = Member{UserID: ownerID, Email: f.users[ownerID], Role: RoleOwner, CreatedAt: team.CreatedAt}
}

func (f *fakeRepository) seedMember(teamID, userID uuid.UUID, role Role) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.members[teamID] == nil {
		f.members[teamID] = map[uuid.UUID]Member{}
	}
	f.members[teamID][userID] = Member{UserID: userID, Email: f.users[userID], Role: role, CreatedAt: time.Now().UTC()}
}

func (f *fakeRepository) member(teamID, userID uuid.UUID) (Member, bool) {
	byUser, ok := f.members[teamID]
	if !ok {
		return Member{}, false
	}
	member, ok := byUser[userID]
	return member, ok
}

func (f *fakeRepository) DeleteTeamIfEmpty(_ context.Context, teamID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.teams[teamID]; !ok {
		return ErrNotFound
	}
	if f.resourceCounts[teamID] > 0 {
		return ErrTeamNotEmpty
	}
	return f.deleteTeam(teamID)
}

func (f *fakeRepository) CountTeamResources(_ context.Context, teamID uuid.UUID) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resourceCounts[teamID], nil
}

func (f *fakeRepository) MutateMembership(_ context.Context, teamID uuid.UUID, fn func(*MembershipTx) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	team, ok := f.teams[teamID]
	if !ok {
		return ErrNotFound
	}
	members := make([]Member, 0, len(f.members[teamID]))
	for _, member := range f.members[teamID] {
		member.Email = f.users[member.UserID]
		members = append(members, member)
	}
	sort.Slice(members, func(i, j int) bool { return members[i].UserID.String() < members[j].UserID.String() })
	tx := &MembershipTx{
		Team:    team,
		Members: members,
		setRole: func(userID uuid.UUID, role Role) (Member, error) {
			member, ok := f.member(teamID, userID)
			if !ok {
				return Member{}, ErrNotFound
			}
			member.Role = role
			member.Email = f.users[userID]
			f.members[teamID][userID] = member
			return member, nil
		},
		remove: func(userID uuid.UUID) error {
			if _, ok := f.member(teamID, userID); !ok {
				return ErrNotFound
			}
			delete(f.members[teamID], userID)
			return nil
		},
	}
	return fn(tx)
}

func (f *fakeRepository) CreateTeam(_ context.Context, team Team, ownerID uuid.UUID) (Team, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if team.CreatedAt.IsZero() {
		team.CreatedAt = time.Now().UTC()
	}
	team.UpdatedAt = team.CreatedAt
	f.teams[team.ID] = team
	f.members[team.ID] = map[uuid.UUID]Member{
		ownerID: {UserID: ownerID, Email: f.users[ownerID], Role: RoleOwner, CreatedAt: team.CreatedAt},
	}
	return team, nil
}

func (f *fakeRepository) GetTeam(_ context.Context, teamID uuid.UUID) (Team, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	team, ok := f.teams[teamID]
	if !ok {
		return Team{}, ErrNotFound
	}
	return team, nil
}

func (f *fakeRepository) GetTeamForUser(_ context.Context, teamID, userID uuid.UUID) (Team, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	team, ok := f.teams[teamID]
	if !ok {
		return Team{}, ErrNotFound
	}
	member, ok := f.member(teamID, userID)
	if !ok {
		return Team{}, ErrNotFound
	}
	team.Role = member.Role
	return team, nil
}

func (f *fakeRepository) ListTeamsByUser(_ context.Context, userID uuid.UUID) ([]Team, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []Team{}
	for teamID, byUser := range f.members {
		member, ok := byUser[userID]
		if !ok {
			continue
		}
		team := f.teams[teamID]
		team.Role = member.Role
		out = append(out, team)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsPersonal != out[j].IsPersonal {
			return !out[i].IsPersonal
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (f *fakeRepository) RenameTeam(_ context.Context, teamID uuid.UUID, name string) (Team, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	team, ok := f.teams[teamID]
	if !ok {
		return Team{}, ErrNotFound
	}
	team.Name = name
	team.UpdatedAt = time.Now().UTC()
	f.teams[teamID] = team
	return team, nil
}

func (f *fakeRepository) DeleteTeam(_ context.Context, teamID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.teams[teamID]; !ok {
		return ErrNotFound
	}
	return f.deleteTeam(teamID)
}

func (f *fakeRepository) deleteTeam(teamID uuid.UUID) error {
	delete(f.teams, teamID)
	delete(f.resourceCounts, teamID)
	delete(f.members, teamID)
	for id, invite := range f.invites {
		if invite.invite.TeamID == teamID {
			delete(f.invites, id)
		}
	}
	return nil
}

func (f *fakeRepository) CreateMember(_ context.Context, teamID, userID uuid.UUID, role Role) (Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.member(teamID, userID); ok {
		return Member{}, ErrConflict
	}
	if f.members[teamID] == nil {
		f.members[teamID] = map[uuid.UUID]Member{}
	}
	member := Member{UserID: userID, Email: f.users[userID], Role: role, CreatedAt: time.Now().UTC()}
	f.members[teamID][userID] = member
	return member, nil
}

func (f *fakeRepository) GetMember(_ context.Context, teamID, userID uuid.UUID) (Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	member, ok := f.member(teamID, userID)
	if !ok {
		return Member{}, ErrNotFound
	}
	return member, nil
}

func (f *fakeRepository) ListMembers(_ context.Context, teamID uuid.UUID) ([]Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.teams[teamID]; !ok {
		return nil, ErrNotFound
	}
	out := []Member{}
	for _, member := range f.members[teamID] {
		member.Email = f.users[member.UserID]
		out = append(out, member)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (f *fakeRepository) UpdateMemberRole(_ context.Context, teamID, userID uuid.UUID, role Role) (Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	member, ok := f.member(teamID, userID)
	if !ok {
		return Member{}, ErrNotFound
	}
	member.Role = role
	f.members[teamID][userID] = member
	return member, nil
}

func (f *fakeRepository) DeleteMember(_ context.Context, teamID, userID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.member(teamID, userID); !ok {
		return ErrNotFound
	}
	delete(f.members[teamID], userID)
	return nil
}

func (f *fakeRepository) CountOwners(_ context.Context, teamID uuid.UUID) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var owners int64
	for _, member := range f.members[teamID] {
		if member.Role == RoleOwner {
			owners++
		}
	}
	return owners, nil
}

func (f *fakeRepository) CreateInvite(_ context.Context, invite Invite, tokenHash string) (Invite, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, stored := range f.invites {
		if stored.hash == tokenHash {
			return Invite{}, ErrConflict
		}
	}
	f.invites[invite.ID] = storedInvite{invite: invite, hash: tokenHash}
	return invite, nil
}

func (f *fakeRepository) ListInvites(_ context.Context, teamID uuid.UUID) ([]Invite, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []Invite{}
	for _, stored := range f.invites {
		if stored.invite.TeamID == teamID {
			out = append(out, stored.invite)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (f *fakeRepository) GetInviteByTokenHash(_ context.Context, tokenHash string) (Invite, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, stored := range f.invites {
		if stored.hash == tokenHash {
			return stored.invite, nil
		}
	}
	return Invite{}, ErrNotFound
}

func (f *fakeRepository) AcceptInvite(_ context.Context, tokenHash string, userID uuid.UUID) (Invite, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, stored := range f.invites {
		if stored.hash != tokenHash {
			continue
		}
		if stored.invite.AcceptedAt != nil {
			return Invite{}, ErrInviteUsed
		}
		if !time.Now().Before(stored.invite.ExpiresAt) {
			return Invite{}, ErrInviteExpired
		}
		if _, ok := f.member(stored.invite.TeamID, userID); ok {
			return Invite{}, ErrConflict
		}
		accepted := time.Now().UTC()
		stored.invite.AcceptedAt = &accepted
		f.invites[id] = stored
		if f.members[stored.invite.TeamID] == nil {
			f.members[stored.invite.TeamID] = map[uuid.UUID]Member{}
		}
		f.members[stored.invite.TeamID][userID] = Member{
			UserID: userID, Email: f.users[userID], Role: stored.invite.Role, CreatedAt: accepted,
		}
		return stored.invite, nil
	}
	return Invite{}, ErrInviteUsed
}

func (f *fakeRepository) DeleteInvite(_ context.Context, teamID, inviteID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored, ok := f.invites[inviteID]
	if !ok || stored.invite.TeamID != teamID {
		return ErrNotFound
	}
	delete(f.invites, inviteID)
	return nil
}

func (f *fakeRepository) UserEmail(_ context.Context, userID uuid.UUID) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	email, ok := f.users[userID]
	if !ok {
		return "", ErrNotFound
	}
	return email, nil
}

func (f *fakeRepository) inviteFor(token string) (Invite, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, stored := range f.invites {
		if stored.hash == hashInviteToken(token) {
			return stored.invite, true
		}
	}
	return Invite{}, false
}

func (f *fakeRepository) expireInvite(inviteID uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored, ok := f.invites[inviteID]
	if !ok {
		panic(fmt.Sprintf("invite %s not found", inviteID))
	}
	stored.invite.ExpiresAt = time.Now().Add(-time.Minute)
	f.invites[inviteID] = stored
}
