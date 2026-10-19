package entity

type User struct {
	UserID       int64  `json:"user_id"`
	Username     string `json:"username"`
	FullName     string `json:"full_name"`
	InviteUserID *int64 `json:"invite_user_id,omitempty"`

	InviteCode string `json:"-"`
}

func (u User) IsHead() bool { return u.InviteUserID == nil }
