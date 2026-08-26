package domain

import "slices"

type Role string

const (
	RoleCollector      Role = "collector"
	RoleResearcher     Role = "researcher"
	RoleReviewer       Role = "reviewer"
	RoleKnowledgeOwner Role = "knowledge_owner"
)

var validRoles = []Role{RoleCollector, RoleResearcher, RoleReviewer, RoleKnowledgeOwner}

func (r Role) Valid() bool { return slices.Contains(validRoles, r) }

func (r Role) CanRegisterSource() bool {
	return r == RoleCollector || r == RoleResearcher || r == RoleKnowledgeOwner
}

func (r Role) CanExtractClaims() bool {
	return r == RoleResearcher || r == RoleKnowledgeOwner
}

func (r Role) CanReview() bool {
	return r == RoleReviewer || r == RoleKnowledgeOwner
}

func (r Role) CanPublish() bool { return r == RoleKnowledgeOwner }

func (r Role) CanCorrect() bool {
	return r == RoleResearcher || r == RoleKnowledgeOwner
}

func (r Role) CanWithdraw() bool { return r == RoleKnowledgeOwner }

func Roles() []Role { return slices.Clone(validRoles) }
