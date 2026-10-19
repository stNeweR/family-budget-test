package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/stNeweR/family-budget/internal/entity"
)

const capabilities = `Просто пиши мне обычным текстом, например:
- «кофе 350» — запишу трату
- «такси 1200 вчера» — учту дату
- «сколько потратил на еду за месяц» — покажу статистику
- «покажи траты за неделю» — список за период
- «последнее было не 350, а 450» — исправлю
- «удали такси» — удалю запись`

func (s *Service) handleCommand(ctx context.Context, user entity.User, cmd, args string) string {
	reply, err := s.runCommand(ctx, user, cmd, args)
	if err != nil {
		s.log.Error("bot command", "command", cmd, "user_id", user.UserID, "error", err)
		return "Извините, не удалось выполнить команду. Попробуйте ещё раз."
	}
	return reply
}

func (s *Service) runCommand(ctx context.Context, user entity.User, cmd, args string) (string, error) {
	switch cmd {
	case "start":
		return s.runStart(ctx, user, args)
	case "invite":
		return s.runInvite(ctx, user)
	case "panel":
		return s.runPanel(user), nil
	default:
		fmt.Printf("[bot] неизвестная команда /%s от %s\n", cmd, userLabel(user))
		return commandsHelp(), nil
	}
}

func (s *Service) runStart(ctx context.Context, user entity.User, code string) (string, error) {
	res, err := s.family.Start(ctx, user, code)
	if err != nil {
		return "", err
	}

	switch res.Outcome {
	case entity.StartHead:
		fmt.Printf("[bot] /start: %s — глава семьи\n", userLabel(user))
		return headWelcome(user), nil
	case entity.StartMember:
		fmt.Printf("[bot] /start: %s уже в семье (%s)\n", userLabel(user), userLabel(res.Head))
		return memberWelcome(user, res.Head), nil
	case entity.StartInvalidCode:
		fmt.Printf("[bot] /start: %s, код %q не найден\n", userLabel(user), code)
		return "Ссылка приглашения недействительна или устарела.\n\n" + headWelcome(user), nil
	case entity.StartOwnCode:
		return "Это ваша собственная ссылка — вы и так глава этой семьи.", nil
	case entity.StartAlreadyMember:
		return fmt.Sprintf("Вы уже в семье — глава %s.", userLabel(res.Head)), nil
	case entity.StartHeadHasMembers:
		fmt.Printf("[bot] /start: %s — глава с %d участниками, вступление отклонено\n",
			userLabel(user), res.MemberCount)
		return fmt.Sprintf("Вы глава семьи, в которой уже есть участники, поэтому присоединиться к семье %s нельзя.",
			userLabel(res.Head)), nil
	case entity.StartJoined:
		fmt.Printf("[bot] /start по коду: %s вступил в семью %s\n", userLabel(user), userLabel(res.Head))
		return fmt.Sprintf("Готово! Теперь вы в семье — глава %s.\n\n%s", userLabel(res.Head), capabilities), nil
	default:
		return "", fmt.Errorf("unknown start outcome %q", res.Outcome)
	}
}

func (s *Service) runInvite(ctx context.Context, user entity.User) (string, error) {
	res, err := s.family.Invite(ctx, user.UserID)
	if err != nil {
		return "", err
	}
	if res.Code == "" {
		return "Ссылка пока недоступна: глава семьи ещё не активировал бота командой /start.", nil
	}

	username, err := s.botUsername(ctx)
	if err != nil {
		return "", err
	}
	link := fmt.Sprintf("https://t.me/%s?start=%s", username, res.Code)

	if res.Head.UserID == user.UserID {
		return "Отправьте эту ссылку тем, кого хотите добавить в вашу семью:\n" + link, nil
	}
	return fmt.Sprintf("Отправьте эту ссылку тем, кого хотите добавить в семью (глава — %s):\n%s",
		userLabel(res.Head), link), nil
}

func (s *Service) runPanel(user entity.User) string {
	fmt.Printf("[bot] /panel: ссылка выдана %s\n", userLabel(user))
	return fmt.Sprintf("Веб-панель расходов:\n%s", strings.TrimRight(s.panelURL, "/"))
}

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

func greeting(user entity.User) string {
	name := user.FullName
	if name == "" {
		name = user.Username
	}
	if name == "" {
		return "Привет"
	}
	return "Привет, " + name
}

func headWelcome(user entity.User) string {
	return greeting(user) + `! Я бот семейного бюджета. Вы — глава семьи.

` + capabilities + `

Семейные команды:
/invite — ссылка, чтобы добавить близких в вашу семью
/panel — веб-панель с расходами семьи`
}

func memberWelcome(user entity.User, head entity.User) string {
	return greeting(user) + fmt.Sprintf(`! Я бот семейного бюджета. Вы в семье, глава — %s.

%s

/invite — ссылка, чтобы позвать в семью ещё кого-нибудь.`, userLabel(head), capabilities)
}
