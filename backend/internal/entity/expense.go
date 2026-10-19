package entity

import (
	"time"

	"github.com/google/uuid"
)

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

var AllCategories = []Category{
	CategoryFood,
	CategoryTransport,
	CategoryHousing,
	CategoryFun,
	CategoryHealth,
	CategoryShopping,
	CategoryOther,
}

func (c Category) Valid() bool {
	for _, known := range AllCategories {
		if c == known {
			return true
		}
	}
	return false
}

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

type ExpensePatch struct {
	Amount   *float64   `json:"amount,omitempty"`
	Category *Category  `json:"category,omitempty"`
	SpentAt  *time.Time `json:"spent_at,omitempty"`
	Comment  *string    `json:"comment,omitempty"`
}

func (p ExpensePatch) Empty() bool {
	return p.Amount == nil && p.Category == nil && p.SpentAt == nil && p.Comment == nil
}

type ExpenseFilter struct {
	UserID   *int64
	From     *time.Time
	To       *time.Time
	Category *Category
	Limit    int
	Offset   int
}
