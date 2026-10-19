package restapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/stNeweR/family-budget/internal/usecase"
)

type usersController struct {
	users usecase.Users
}

func newUsersController(users usecase.Users) *usersController {
	return &usersController{
		users: users,
	}
}

func (c *usersController) Register(r chi.Router) {
	r.Get("/users", c.listUsers)
}

func (c *usersController) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := c.users.ListUsers(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}
