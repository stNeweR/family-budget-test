package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/stNeweR/family-budget/config"
	"github.com/stNeweR/family-budget/internal/entity"
	expenserepo "github.com/stNeweR/family-budget/internal/repo/persistent/expense"
	userrepo "github.com/stNeweR/family-budget/internal/repo/persistent/user"
	"github.com/stNeweR/family-budget/pkg/postgres"
)

type catProfile struct {
	category entity.Category
	comments []string
	min      int
	max      int
}

var profiles = []catProfile{
	{entity.CategoryFood, []string{"кофе", "обед в кафе", "продукты на неделю", "пицца", "завтрак"}, 150, 4000},
	{entity.CategoryTransport, []string{"такси", "проездной", "бензин", "метро"}, 60, 3500},
	{entity.CategoryHousing, []string{"квартплата", "интернет", "электричество", "ремонт розетки"}, 500, 12000},
	{entity.CategoryFun, []string{"кино", "настольная игра", "подписка", "концерт"}, 300, 5000},
	{entity.CategoryHealth, []string{"аптека", "зубной врач", "витамины", "анализы"}, 300, 8000},
	{entity.CategoryShopping, []string{"футболка", "книга", "посуда", "зарядка для телефона"}, 400, 9000},
	{entity.CategoryOther, []string{"подарок", "непредвиденное", "бытовая мелочь"}, 200, 3000},
}

var demoMembers = []entity.User{
	{UserID: 1000000001, Username: "mama", FullName: "Анна Петрова"},
	{UserID: 1000000002, Username: "papa", FullName: "Игорь Петров"},
	{UserID: 1000000003, Username: "masha", FullName: "Маша Петрова"},
	{UserID: 1000000004, Username: "ded", FullName: "Виктор Петров"},
	{UserID: 1000000005, Username: "baba", FullName: "Людмила Петрова"},
}

const devInviteCode = "dev-invite-code"

func main() {
	var (
		reset   = flag.Bool("reset", false, "truncate tables before seeding")
		users   = flag.Int("users", 3, "number of family members")
		perUser = flag.Int("per-user", 40, "expenses per member")
		days    = flag.Int("days", 90, "spread expenses over the last N days")
		seed    = flag.Int64("seed", 1, "random seed for reproducible data")
	)
	flag.Parse()

	if err := run(*reset, *users, *perUser, *days, *seed); err != nil {
		fmt.Fprintf(os.Stderr, "seed: %v\n", err)
		os.Exit(1)
	}
}

func run(reset bool, usersCount, perUser, days int, seed int64) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx := context.Background()

	pg, err := postgres.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pg.Close()

	if err := pg.CheckSchema(ctx); err != nil {
		return err
	}

	users := userrepo.New(pg)
	expenses := expenserepo.New(pg)

	if reset {
		if err := pg.Reset(ctx); err != nil {
			return err
		}
	} else if existing, err := users.ListUsers(ctx); err != nil {
		return err
	} else if len(existing) > 0 {
		fmt.Printf("already seeded (%d members). Run with -reset to start over.\n", len(existing))
		return nil
	}

	rnd := rand.New(rand.NewSource(seed))
	members := pickMembers(usersCount)

	for _, member := range members {
		if err := users.UpsertUser(ctx, member); err != nil {
			return err
		}
	}

	head := members[0]
	if err := users.BecomeHead(ctx, head.UserID, devInviteCode); err != nil {
		return err
	}
	for _, member := range members[1:] {
		if err := users.JoinFamily(ctx, member.UserID, head.UserID); err != nil {
			return err
		}
	}

	created := 0
	now := time.Now().UTC().Truncate(24 * time.Hour)
	for _, member := range members {
		for i := 0; i < perUser; i++ {
			profile := profiles[rnd.Intn(len(profiles))]
			amount := float64(profile.min + rnd.Intn(profile.max-profile.min))
			spentAt := now.AddDate(0, 0, -rnd.Intn(days))
			expense := entity.Expense{
				UserID:   member.UserID,
				Amount:   amount,
				Category: profile.category,
				SpentAt:  spentAt,
				Comment:  profile.comments[rnd.Intn(len(profile.comments))],
			}
			if err := expenses.CreateExpense(ctx, &expense); err != nil {
				return err
			}
			created++
		}
	}

	fmt.Printf("seeded %d members and %d expenses (seed=%d, last %d days)\n",
		len(members), created, seed, days)
	fmt.Printf("глава семьи: %s (id=%d), участников: %d\n", head.FullName, head.UserID, len(members)-1)
	fmt.Printf("код приглашения: %s\n", devInviteCode)
	fmt.Printf("панель расходов: %s\n", cfg.PanelURL)
	return nil
}

func pickMembers(count int) []entity.User {
	if count <= 0 {
		return nil
	}
	if count > len(demoMembers) {

		generated := make([]entity.User, count)
		copy(generated, demoMembers)
		for i := len(demoMembers); i < count; i++ {
			generated[i] = entity.User{
				UserID:   int64(1000000001 + i),
				Username: fmt.Sprintf("member%d", i+1),
				FullName: fmt.Sprintf("Член семьи %d", i+1),
			}
		}
		return generated
	}
	return demoMembers[:count]
}
