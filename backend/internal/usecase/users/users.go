package users

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/stNeweR/family-budget/internal/entity"
	"github.com/stNeweR/family-budget/internal/repo"
)

const inviteCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

const inviteCodeLength = 10

type UseCase struct {
	repo repo.UserRepo
}

func New(repo repo.UserRepo) *UseCase {
	return &UseCase{repo: repo}
}

func (s *UseCase) EnsureUser(ctx context.Context, user entity.User) (entity.User, error) {
	if err := s.repo.UpsertUser(ctx, user); err != nil {
		return entity.User{}, err
	}
	return s.repo.GetUser(ctx, user.UserID)
}

func (s *UseCase) Start(ctx context.Context, user entity.User, code string) (entity.StartResult, error) {
	stored, err := s.repo.GetUser(ctx, user.UserID)
	if err != nil {
		return entity.StartResult{}, err
	}

	if code == "" {
		stored, err = s.ensureHeadCode(ctx, stored)
		if err != nil {
			return entity.StartResult{}, err
		}
		if !stored.IsHead() {
			head, err := s.repo.GetUser(ctx, *stored.InviteUserID)
			if err != nil {
				return entity.StartResult{}, err
			}
			return entity.StartResult{Outcome: entity.StartMember, User: stored, Head: head}, nil
		}
		return entity.StartResult{Outcome: entity.StartHead, User: stored}, nil
	}

	head, err := s.repo.GetHeadByInviteCode(ctx, code)
	if err != nil && !errors.Is(err, entity.ErrNotFound) {
		return entity.StartResult{}, err
	}
	if errors.Is(err, entity.ErrNotFound) {
		stored, err = s.ensureHeadCode(ctx, stored)
		if err != nil {
			return entity.StartResult{}, err
		}
		return entity.StartResult{Outcome: entity.StartInvalidCode, User: stored}, nil
	}

	if head.UserID == user.UserID {
		return entity.StartResult{Outcome: entity.StartOwnCode, User: stored, Head: head}, nil
	}
	if stored.InviteUserID != nil && *stored.InviteUserID == head.UserID {
		return entity.StartResult{Outcome: entity.StartAlreadyMember, User: stored, Head: head}, nil
	}
	if stored.IsHead() {
		count, err := s.repo.CountFamilyMembers(ctx, stored.UserID)
		if err != nil {
			return entity.StartResult{}, err
		}
		if count > 0 {
			return entity.StartResult{Outcome: entity.StartHeadHasMembers, User: stored, Head: head, MemberCount: count}, nil
		}
	}

	if err := s.repo.JoinFamily(ctx, stored.UserID, head.UserID); err != nil {
		if errors.Is(err, entity.ErrNotFound) {
			return entity.StartResult{Outcome: entity.StartInvalidCode, User: stored}, nil
		}
		return entity.StartResult{}, err
	}
	return entity.StartResult{Outcome: entity.StartJoined, User: stored, Head: head}, nil
}

func (s *UseCase) Invite(ctx context.Context, userID int64) (entity.InviteResult, error) {
	stored, err := s.repo.GetUser(ctx, userID)
	if err != nil {
		return entity.InviteResult{}, err
	}

	head := stored
	if head.IsHead() {
		head, err = s.ensureHeadCode(ctx, head)
		if err != nil {
			return entity.InviteResult{}, err
		}
	} else {
		head, err = s.repo.GetUser(ctx, *head.InviteUserID)
		if err != nil {
			return entity.InviteResult{}, err
		}
	}
	return entity.InviteResult{Head: head, Code: head.InviteCode}, nil
}

func (s *UseCase) ListUsers(ctx context.Context) ([]entity.User, error) {
	return s.repo.ListUsers(ctx)
}

func (s *UseCase) ListFamilyUsers(ctx context.Context, headID int64) ([]entity.User, error) {
	return s.repo.ListFamilyUsers(ctx, headID)
}

func (s *UseCase) ensureHeadCode(ctx context.Context, user entity.User) (entity.User, error) {
	if !user.IsHead() || user.InviteCode != "" {
		return user, nil
	}
	code, err := newInviteCode()
	if err != nil {
		return entity.User{}, err
	}
	if err := s.repo.BecomeHead(ctx, user.UserID, code); err != nil {
		return entity.User{}, err
	}
	user.InviteCode = code
	return user, nil
}

func newInviteCode() (string, error) {
	buf := make([]byte, inviteCodeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate invite code: %w", err)
	}
	for i, b := range buf {
		buf[i] = inviteCodeAlphabet[int(b)%len(inviteCodeAlphabet)]
	}
	return string(buf), nil
}
