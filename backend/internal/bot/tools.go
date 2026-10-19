package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/stNeweR/family-budget/internal/domain"
	"github.com/stNeweR/family-budget/internal/llm"
)

// Tool names exposed to the model.
const (
	toolAddExpense    = "add_expense"
	toolListExpenses  = "list_expenses"
	toolGetTotal      = "get_total"
	toolUpdateExpense = "update_expense"
	toolDeleteExpense = "delete_expense"
)

// tools returns the function definitions advertised to the LLM.
func tools() []llm.Tool {
	return []llm.Tool{
		llm.NewFunctionTool(toolAddExpense, "Добавить новую трату пользователя.", schemaObject(map[string]any{
			"amount":   schemaNumber("Сумма траты в рублях, положительное число."),
			"category": schemaEnum("Категория траты.", categoryValues()),
			"spent_at": schemaString("Дата траты в формате YYYY-MM-DD. Если не указана — сегодня."),
			"comment":  schemaString("Необязательный короткий комментарий."),
		}, []string{"amount", "category"})),

		llm.NewFunctionTool(toolListExpenses, "Показать список трат пользователя. Используй, чтобы найти id траты перед изменением или удалением.", schemaObject(map[string]any{
			"from":     schemaString("Начало периода YYYY-MM-DD (включительно)."),
			"to":       schemaString("Конец периода YYYY-MM-DD (включительно)."),
			"category": schemaEnum("Фильтр по категории.", categoryValues()),
			"limit":    schemaInteger("Сколько последних трат вернуть (по умолчанию 20)."),
		}, nil)),

		llm.NewFunctionTool(toolGetTotal, "Посчитать сумму трат за период, опционально по категории.", schemaObject(map[string]any{
			"from":     schemaString("Начало периода YYYY-MM-DD (включительно)."),
			"to":       schemaString("Конец периода YYYY-MM-DD (включительно)."),
			"category": schemaEnum("Фильтр по категории.", categoryValues()),
		}, nil)),

		llm.NewFunctionTool(toolUpdateExpense, "Изменить существующую трату по её id. Сначала найди id через list_expenses.", schemaObject(map[string]any{
			"id":       schemaString("Идентификатор траты (UUID), полученный из list_expenses."),
			"amount":   schemaNumber("Новая сумма."),
			"category": schemaEnum("Новая категория.", categoryValues()),
			"spent_at": schemaString("Новая дата YYYY-MM-DD."),
			"comment":  schemaString("Новый комментарий."),
		}, []string{"id"})),

		llm.NewFunctionTool(toolDeleteExpense, "Удалить трату по её id. Сначала найди id через list_expenses.", schemaObject(map[string]any{
			"id": schemaString("Идентификатор траты (UUID), полученный из list_expenses."),
		}, []string{"id"})),
	}
}

type (
	addExpenseArgs struct {
		Amount   float64 `json:"amount"`
		Category string  `json:"category"`
		SpentAt  string  `json:"spent_at"`
		Comment  string  `json:"comment"`
	}
	listExpensesArgs struct {
		From     string `json:"from"`
		To       string `json:"to"`
		Category string `json:"category"`
		Limit    int    `json:"limit"`
	}
	getTotalArgs struct {
		From     string `json:"from"`
		To       string `json:"to"`
		Category string `json:"category"`
	}
	updateExpenseArgs struct {
		ID       string   `json:"id"`
		Amount   *float64 `json:"amount"`
		Category string   `json:"category"`
		SpentAt  string   `json:"spent_at"`
		Comment  *string  `json:"comment"`
	}
	deleteExpenseArgs struct {
		ID string `json:"id"`
	}
)

// executeTool runs a single tool call on behalf of the user and returns a
// human/model readable result. All store access is scoped to userID so a user
// can never touch another member's expenses.
func (a *Agent) executeTool(ctx context.Context, user domain.User, name, rawArgs string) (string, error) {
	switch name {
	case toolAddExpense:
		return a.toolAdd(ctx, user, rawArgs)
	case toolListExpenses:
		return a.toolList(ctx, user, rawArgs)
	case toolGetTotal:
		return a.toolTotal(ctx, user, rawArgs)
	case toolUpdateExpense:
		return a.toolUpdate(ctx, user, rawArgs)
	case toolDeleteExpense:
		return a.toolDelete(ctx, user, rawArgs)
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

func (a *Agent) toolAdd(ctx context.Context, user domain.User, rawArgs string) (string, error) {
	var args addExpenseArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "", fmt.Errorf("parse %s args: %w", toolAddExpense, err)
	}
	if args.Amount <= 0 {
		return "Сумма должна быть положительной. Уточните сумму траты.", nil
	}
	spentAt, err := parseDate(args.SpentAt, a.today())
	if err != nil {
		return "", err
	}
	expense := domain.Expense{
		UserID:   user.UserID,
		Amount:   args.Amount,
		Category: normalizeCategory(args.Category),
		SpentAt:  spentAt,
		Comment:  strings.TrimSpace(args.Comment),
	}
	if err := a.store.CreateExpense(ctx, &expense); err != nil {
		return "", err
	}
	return fmt.Sprintf("Сохранено: id=%s, %.2f ₽, %s, %s, комментарий=%q",
		expense.ID, expense.Amount, expense.Category, expense.SpentAt.Format(dateLayout), expense.Comment), nil
}

func (a *Agent) toolList(ctx context.Context, user domain.User, rawArgs string) (string, error) {
	var args listExpensesArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "", fmt.Errorf("parse %s args: %w", toolListExpenses, err)
	}
	filter, err := a.buildFilter(user, args.From, args.To, args.Category, args.Limit)
	if err != nil {
		return "", err
	}
	expenses, err := a.store.ListExpenses(ctx, filter)
	if err != nil {
		return "", err
	}
	if len(expenses) == 0 {
		return "Трат за указанный период не найдено.", nil
	}
	var sb strings.Builder
	sb.WriteString("Траты (новые сверху):\n")
	var total float64
	for _, e := range expenses {
		total += e.Amount
		fmt.Fprintf(&sb, "- id=%s | %s | %.2f ₽ | %s",
			e.ID, e.SpentAt.Format(dateLayout), e.Amount, e.Category)
		if e.Comment != "" {
			fmt.Fprintf(&sb, " | %s", e.Comment)
		}
		sb.WriteString("\n")
	}
	fmt.Fprintf(&sb, "Итого: %.2f ₽ за %d шт.", total, len(expenses))
	return sb.String(), nil
}

func (a *Agent) toolTotal(ctx context.Context, user domain.User, rawArgs string) (string, error) {
	var args getTotalArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "", fmt.Errorf("parse %s args: %w", toolGetTotal, err)
	}
	filter, err := a.buildFilter(user, args.From, args.To, args.Category, 500)
	if err != nil {
		return "", err
	}
	expenses, err := a.store.ListExpenses(ctx, filter)
	if err != nil {
		return "", err
	}
	var total float64
	for _, e := range expenses {
		total += e.Amount
	}
	scope := "за весь период"
	if args.Category != "" {
		scope = fmt.Sprintf("по категории %q", normalizeCategory(args.Category))
	}
	return fmt.Sprintf("Итого %s: %.2f ₽ (%d трат).", scope, total, len(expenses)), nil
}

func (a *Agent) toolUpdate(ctx context.Context, user domain.User, rawArgs string) (string, error) {
	var args updateExpenseArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "", fmt.Errorf("parse %s args: %w", toolUpdateExpense, err)
	}
	id, err := uuid.Parse(strings.TrimSpace(args.ID))
	if err != nil {
		return "", fmt.Errorf("invalid expense id %q", args.ID)
	}
	if _, err := a.ownedExpense(ctx, user.UserID, id); err != nil {
		return notFoundMessage(err), nil
	}

	patch := domain.ExpensePatch{Amount: args.Amount, Comment: args.Comment}
	if args.Category != "" {
		c := normalizeCategory(args.Category)
		patch.Category = &c
	}
	if args.SpentAt != "" {
		spentAt, err := parseDate(args.SpentAt, a.today())
		if err != nil {
			return "", err
		}
		patch.SpentAt = &spentAt
	}
	if patch.Empty() {
		return "Не указано, что именно изменить.", nil
	}

	updated, err := a.store.UpdateExpense(ctx, id, patch)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Обновлено: id=%s | %s | %.2f ₽ | %s | %q",
		updated.ID, updated.SpentAt.Format(dateLayout), updated.Amount, updated.Category, updated.Comment), nil
}

func (a *Agent) toolDelete(ctx context.Context, user domain.User, rawArgs string) (string, error) {
	var args deleteExpenseArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "", fmt.Errorf("parse %s args: %w", toolDeleteExpense, err)
	}
	id, err := uuid.Parse(strings.TrimSpace(args.ID))
	if err != nil {
		return "", fmt.Errorf("invalid expense id %q", args.ID)
	}
	expense, err := a.ownedExpense(ctx, user.UserID, id)
	if err != nil {
		return notFoundMessage(err), nil
	}
	if err := a.store.DeleteExpense(ctx, id); err != nil {
		return "", err
	}
	return fmt.Sprintf("Удалено: id=%s | %.2f ₽ | %s", expense.ID, expense.Amount, expense.Category), nil
}

// ownedExpense fetches an expense and makes sure it belongs to the user.
// Foreign expenses are reported as not found so ownership is never leaked.
func (a *Agent) ownedExpense(ctx context.Context, userID int64, id uuid.UUID) (domain.Expense, error) {
	expense, err := a.store.GetExpense(ctx, id)
	if err != nil {
		return domain.Expense{}, err
	}
	if expense.UserID != userID {
		return domain.Expense{}, domain.ErrNotFound
	}
	return expense, nil
}

func notFoundMessage(err error) string {
	if err == domain.ErrNotFound {
		return "Трата не найдена. Возможно, она принадлежит другому члену семьи или уже удалена."
	}
	return err.Error()
}

func (a *Agent) buildFilter(user domain.User, from, to, category string, limit int) (domain.ExpenseFilter, error) {
	if limit <= 0 || limit > 500 {
		limit = 20
	}
	uid := user.UserID
	filter := domain.ExpenseFilter{UserID: &uid, Limit: limit}
	if from != "" {
		d, err := parseDate(from, a.today())
		if err != nil {
			return filter, err
		}
		filter.From = &d
	}
	if to != "" {
		d, err := parseDate(to, a.today())
		if err != nil {
			return filter, err
		}
		filter.To = &d
	}
	if category != "" {
		c := normalizeCategory(category)
		filter.Category = &c
	}
	return filter, nil
}

// dateLayout is the wire format for dates exchanged with the model.
const dateLayout = "2006-01-02"

func (a *Agent) today() time.Time {
	return a.now().UTC().Truncate(24 * time.Hour)
}

func parseDate(value string, fallback time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	for _, layout := range []string{dateLayout, time.RFC3339} {
		if d, err := time.Parse(layout, value); err == nil {
			return d, nil
		}
	}
	return time.Time{}, fmt.Errorf("не удалось разобрать дату %q, ожидается YYYY-MM-DD", value)
}

// normalizeCategory maps free-form model output to a known category,
// falling back to "другое".
func normalizeCategory(value string) domain.Category {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return domain.CategoryOther
	}
	c := domain.Category(value)
	if c.Valid() {
		return c
	}
	for _, known := range domain.AllCategories {
		if strings.Contains(value, string(known)) {
			return known
		}
	}
	return domain.CategoryOther
}

func categoryValues() []string {
	values := make([]string, 0, len(domain.AllCategories))
	for _, c := range domain.AllCategories {
		values = append(values, string(c))
	}
	sort.Strings(values)
	return values
}

func schemaObject(properties map[string]any, required []string) json.RawMessage {
	obj := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		obj["required"] = required
	}
	raw, _ := json.Marshal(obj)
	return raw
}

func schemaString(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func schemaNumber(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}

func schemaInteger(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func schemaEnum(description string, values []string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
		"enum":        values,
	}
}
