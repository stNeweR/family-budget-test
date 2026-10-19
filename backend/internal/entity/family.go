package entity

type StartOutcome string

const (
	StartHead           StartOutcome = "head"
	StartMember         StartOutcome = "member"
	StartJoined         StartOutcome = "joined"
	StartInvalidCode    StartOutcome = "invalid_code"
	StartOwnCode        StartOutcome = "own_code"
	StartAlreadyMember  StartOutcome = "already_member"
	StartHeadHasMembers StartOutcome = "head_has_members"
)

type StartResult struct {
	Outcome     StartOutcome
	User        User
	Head        User
	MemberCount int
}

type InviteResult struct {
	Head User
	Code string
}
