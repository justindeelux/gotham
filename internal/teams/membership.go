package teams

import "github.com/google/uuid"

type MembershipTx struct {
	Team    Team
	Members []Member

	setRole func(userID uuid.UUID, role Role) (Member, error)
	remove  func(userID uuid.UUID) error
}

func (tx *MembershipTx) Member(userID uuid.UUID) (Member, bool) {
	for _, member := range tx.Members {
		if member.UserID == userID {
			return member, true
		}
	}
	return Member{}, false
}

func (tx *MembershipTx) Owners() int {
	owners := 0
	for _, member := range tx.Members {
		if member.Role == RoleOwner {
			owners++
		}
	}
	return owners
}

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

func emailOf(members []Member, userID uuid.UUID) string {
	for _, member := range members {
		if member.UserID == userID {
			return member.Email
		}
	}
	return ""
}
