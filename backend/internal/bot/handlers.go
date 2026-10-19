package bot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/stNeweR/family-budget/internal/domain"
	"github.com/stNeweR/family-budget/internal/store"
)

// capabilities is the shared "what I can do" text for welcome replies.
const capabilities = `Просто пиши мне обычным текстом, например:
- «кофе 350» — запишу трату
- «такси 1200 вчера» — учту дату
- «сколько потратил на еду за месяц» — покажу статистику
- «покажи траты за неделю» — список за период
- «последнее было не 350, а 450» — исправлю
- «удали такси» — удалю запись`

// inviteCodeAlphabet excludes easily confused characters (0/O, 1/I/L).
const inviteCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

const inviteCodeLength = 10

// handleCommand dispatches a bot command and never fails: internal errors are
// logged and turned into a generic reply.
func (s *Service) handleCommand(ctx context.Context, user domain.User, cmd, args string) string {
	reply, err := s.runCommand(ctx, user, cmd, args)
	if err != nil {
		s.log.Error("bot command", "command", cmd, "user_id", user.UserID, "error", err)
		return "Извините, не удалось выполнить команду. Попробуйте ещё раз."
	}
	return reply
}

func (s *Service) runCommand(ctx context.Context, user domain.User, cmd, args string) (string, error) {
	switch cmd {
	case "start":
		return s.runStart(ctx, user, args)
	case "invite":
		return s.runInvite(ctx, user)
	case "panel":
		return s.runPanel(ctx, user)
	default:
		fmt.Printf("[bot] неизвестная команда /%s от %s\n", cmd, userLabel(user))
		return commandsHelp(), nil
	}
}

// runStart handles /start. Without a payload the user becomes a family head
// (or keeps the current role); with a payload — the invite code of a head —
// the user joins that family as a member.
func (s *Service) runStart(ctx context.Context, user domain.User, payload string) (string, error) {
	stored, err := s.store.GetUser(ctx, user.UserID)
	if err != nil {
		return "", err
	}

	if payload == "" {
		stored, err = s.ensureHeadCredentials(ctx, stored)
		if err != nil {
			return "", err
		}
		if !stored.IsHead() {
			head, err := s.store.GetUser(ctx, *stored.InviteUserID)
			if err != nil {
				return "", err
			}
			fmt.Printf("[bot] /start: %s уже в семье (%s)\n", userLabel(user), userLabel(head))
			return memberWelcome(user, head), nil
		}
		fmt.Printf("[bot] /start: %s — глава семьи\n", userLabel(user))
		return headWelcome(user), nil
	}

	head, err := s.store.GetHeadByInviteCode(ctx, payload)
	if err != nil && !store.IsNotFound(err) {
		return "", err
	}
	if store.IsNotFound(err) {
		fmt.Printf("[bot] /start: %s, код %q не найден\n", userLabel(user), payload)
		if _, err := s.ensureHeadCredentials(ctx, stored); err != nil {
			return "", err
		}
		return "Ссылка приглашения недействительна или устарела.\n\n" + headWelcome(user), nil
	}

	if head.UserID == user.UserID {
		return "Это ваша собственная ссылка — вы и так глава этой семьи.", nil
	}
	if stored.InviteUserID != nil && *stored.InviteUserID == head.UserID {
		return fmt.Sprintf("Вы уже в семье — глава %s.", userLabel(head)), nil
	}
	if stored.IsHead() {
		count, err := s.store.CountFamilyMembers(ctx, stored.UserID)
		if err != nil {
			return "", err
		}
		if count > 0 {
			fmt.Printf("[bot] /start: %s — глава с %d участниками, вступление отклонено\n",
				userLabel(user), count)
			return fmt.Sprintf("Вы глава семьи, в которой уже есть участники, поэтому присоединиться к семье %s нельзя.",
				userLabel(head)), nil
		}
	}

	if err := s.store.JoinFamily(ctx, stored.UserID, head.UserID); err != nil {
		if store.IsNotFound(err) {
			return "Ссылка приглашения недействительна или устарела.", nil
		}
		return "", err
	}
	fmt.Printf("[bot] /start по коду: %s вступил в семью %s\n", userLabel(user), userLabel(head))
	return fmt.Sprintf("Готово! Теперь вы в семье — глава %s.\n\n%s", userLabel(head), capabilities), nil
}

// runInvite returns the deep link that adds a new member. For a member the
// link always points at the head's invite code: everything rotates around the
// head.
func (s *Service) runInvite(ctx context.Context, user domain.User) (string, error) {
	stored, err := s.store.GetUser(ctx, user.UserID)
	if err != nil {
		return "", err
	}

	head := stored
	if head.IsHead() {
		head, err = s.ensureHeadCredentials(ctx, head)
		if err != nil {
			return "", err
		}
	} else {
		head, err = s.store.GetUser(ctx, *head.InviteUserID)
		if err != nil {
			return "", err
		}
	}
	if head.InviteCode == "" {
		return "Ссылка пока недоступна: глава семьи ещё не активировал бота командой /start.", nil
	}

	username, err := s.botUsername(ctx)
	if err != nil {
		return "", err
	}
	link := fmt.Sprintf("https://t.me/%s?start=%s", username, head.InviteCode)

	if stored.IsHead() {
		return "Отправьте эту ссылку тем, кого хотите добавить в вашу семью:\n" + link, nil
	}
	return fmt.Sprintf("Отправьте эту ссылку тем, кого хотите добавить в семью (глава — %s):\n%s",
		userLabel(head), link), nil
}

// runPanel returns the web panel link. Only the head may open the panel.
func (s *Service) runPanel(ctx context.Context, user domain.User) (string, error) {
	stored, err := s.store.GetUser(ctx, user.UserID)
	if err != nil {
		return "", err
	}
	if !stored.IsHead() {
		fmt.Printf("[bot] /panel от участника %s — отказ\n", userLabel(user))
		return "Панель расходов доступна только главе семьи. Попросите главу прислать ссылку.", nil
	}

	stored, err = s.ensureHeadCredentials(ctx, stored)
	if err != nil {
		return "", err
	}
	fmt.Printf("[bot] /panel: ссылка выдана %s\n", userLabel(user))
	return fmt.Sprintf("Панель расходов вашей семьи:\n%s/?token=%s\n\nСсылка даёт доступ без пароля — не пересылайте её посторонним.",
		strings.TrimRight(s.panelURL, "/"), stored.PanelToken), nil
}

// ensureHeadCredentials generates the invite code and panel token for a head
// who does not have them yet (e.g. accounts created before the family model
// existed). Members are returned untouched.
func (s *Service) ensureHeadCredentials(ctx context.Context, user domain.User) (domain.User, error) {
	if !user.IsHead() || (user.InviteCode != "" && user.PanelToken != "") {
		return user, nil
	}
	code, err := newInviteCode()
	if err != nil {
		return domain.User{}, err
	}
	token, err := newPanelToken()
	if err != nil {
		return domain.User{}, err
	}
	if err := s.store.BecomeHead(ctx, user.UserID, code, token); err != nil {
		return domain.User{}, err
	}
	user.InviteCode, user.PanelToken = code, token
	fmt.Printf("[bot] сгенерированы креденшелы главы для %s\n", userLabel(user))
	return user, nil
}

// botUsername resolves and caches the bot's @username used in deep links.
func (s *Service) botUsername(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.botUsernameCache != "" {
		return s.botUsernameCache, nil
	}
	me, err := s.tg.GetMe(ctx)
	if err != nil {
		return "", fmt.Errorf("getMe: %w", err)
	}
	if me.Username == "" {
		return "", fmt.Errorf("bot has no username")
	}
	s.botUsernameCache = me.Username
	return me.Username, nil
}

func greeting(user domain.User) string {
	name := user.FullName
	if name == "" {
		name = user.Username
	}
	if name == "" {
		return "Привет"
	}
	return "Привет, " + name
}

func headWelcome(user domain.User) string {
	return greeting(user) + `! Я бот семейного бюджета. Вы — глава семьи.

` + capabilities + `

Семейные команды:
/invite — ссылка, чтобы добавить близких в вашу семью
/panel — веб-панель с расходами семьи`
}

func memberWelcome(user domain.User, head domain.User) string {
	return greeting(user) + fmt.Sprintf(`! Я бот семейного бюджета. Вы в семье, глава — %s.

%s

/invite — ссылка, чтобы позвать в семью ещё кого-нибудь.`, userLabel(head), capabilities)
}

// newInviteCode returns a random code safe to embed into a Telegram deep link.
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

// newPanelToken returns a random secret that unlocks the web panel.
func newPanelToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate panel token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
