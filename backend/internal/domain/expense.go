package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Category is a fixed set of spending categories accepted by the service.
type Category string

const (
	CategoryFood      Category = "еда"
	CategoryTransport Category = "транспорт"
	CategoryHousing   Category = "жильё"
	CategoryFun       Category = "развлечения"
	CategoryHealth    Category = "здоровье"
	CategoryShopping  Category = "покупки"
	CategoryOther     Category = "другое"
)

// AllCategories lists every supported category, mostly for prompts and UI.
var AllCategories = []Category{
	CategoryFood,
	CategoryTransport,
	CategoryHousing,
	CategoryFun,
	CategoryHealth,
	CategoryShopping,
	CategoryOther,
}

// Valid reports whether the category is one of the known values.
func (c Category) Valid() bool {
	for _, known := range AllCategories {
		if c == known {
			return true
		}
	}
	return false
}

// User is a family member identified by their Telegram ID.
type User struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	FullName string `json:"full_name"`
}

// Expense is a single spending record.
type Expense struct {
	ID        uuid.UUID `json:"id"`
	UserID    int64     `json:"user_id"`
	Amount    float64   `json:"amount"`
	Category  Category  `json:"category"`
	SpentAt   time.Time `json:"spent_at"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ExpensePatch is a partial update. Nil fields are left untouched.
type ExpensePatch struct {
	Amount   *float64   `json:"amount,omitempty"`
	Category *Category  `json:"category,omitempty"`
	SpentAt  *time.Time `json:"spent_at,omitempty"`
	Comment  *string    `json:"comment,omitempty"`
}

// Empty reports whether the patch carries no changes.
func (p ExpensePatch) Empty() bool {
	return p.Amount == nil && p.Category == nil && p.SpentAt == nil && p.Comment == nil
}

// ExpenseFilter narrows down a list query. Zero values mean "no restriction".
type ExpenseFilter struct {
	UserID   *int64
	From     *time.Time
	To       *time.Time
	Category *Category
	Limit    int
	Offset   int
}

// Domain errors shared across layers so HTTP and the bot can map them.
var (
	ErrNotFound = errors.New("expense not found")
	ErrInvalid  = errors.New("invalid input")
)
