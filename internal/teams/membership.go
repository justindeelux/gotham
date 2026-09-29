package teams

import "github.com/google/uuid"

// MembershipTx is the locked, transactional view of one team's memberships.
// It is only valid inside the callback passed to Repository.MutateMembership:
// that callback runs while the team row is locked, and the transaction commits
// when it returns nil. Policy code (the last-owner rule, the personal-owner
// invariant, admin limits) therefore reads and writes the same serialized
// snapshot, so two concurrent mutations cannot both observe a valid state and
// then invalidate it.
type MembershipTx struct {
	// Team is the locked team row.
	Team Team
	// Members is the locked membership snapshot, emails included.
	Members []Member

	setRole func(userID uuid.UUID, role Role) (Member, error)
	remove  func(userID uuid.UUID) error
}

// Member returns one membership from the snapshot.
func (tx *MembershipTx) Member(userID uuid.UUID) (Member, bool) {
	for _, member := range tx.Members {
		if member.UserID == userID {
			return member, true
		}
	}
	return Member{}, false
}

// Owners reports how many owners the team has right now.
func (tx *MembershipTx) Owners() int {
	owners := 0
	for _, member := range tx.Members {
		if member.Role == RoleOwner {
			owners++
		}
	}
	return owners
}

// SetRole changes one membership inside the transaction and updates the
// snapshot so later checks in the same callback see the new role.
func (tx *MembershipTx) SetRole(userID uuid.UUID, role Role) (Member, error) {
	updated, err := tx.setRole(userID, role)
	if err != nil {
		return Member{}, err
	}
	updated.Email = emailOf(tx.Members, userID)
	for i := range tx.Members {
		if tx.Members[i].UserID == userID {
			tx.Members[i].Role = role
		}
	}
	return updated, nil
}

// Remove deletes one membership inside the transaction and updates the
// snapshot.
func (tx *MembershipTx) Remove(userID uuid.UUID) error {
	if err := tx.remove(userID); err != nil {
		return err
	}
	kept := tx.Members[:0]
	for _, member := range tx.Members {
		if member.UserID != userID {
			kept = append(kept, member)
		}
	}
	tx.Members = kept
	return nil
}

// emailOf returns the stored email of a member, for responses that echo a
// mutated membership.
func emailOf(members []Member, userID uuid.UUID) string {
	for _, member := range members {
		if member.UserID == userID {
			return member.Email
		}
	}
	return ""
}
