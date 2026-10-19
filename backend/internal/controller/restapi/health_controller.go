package restapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/stNeweR/family-budget/internal/repo"
)

type healthController struct {
	pinger repo.Pinger
}

func newHealthController(pinger repo.Pinger) *healthController {
	return &healthController{pinger: pinger}
}

func (c *healthController) Register(r chi.Router) {
	r.Get("/health", c.handle)
}

func (c *healthController) handle(w http.ResponseWriter, r *http.Request) {
	if err := c.pinger.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
