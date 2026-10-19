package restapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/stNeweR/family-budget/internal/repo"
	"github.com/stNeweR/family-budget/internal/usecase"
)

type apiControllers struct {
	expenses *expensesController
	users    *usersController
	health   *healthController
}

type Server struct {
	controllers    apiControllers
	webhookPath    string
	webhookHandler http.HandlerFunc
}

type Option func(*Server)

func WithWebhook(path string, handler http.HandlerFunc) Option {
	return func(s *Server) {
		s.webhookPath = path
		s.webhookHandler = handler
	}
}

func New(
	expenses usecase.Expense,
	users usecase.Users,
	pinger repo.Pinger,
	opts ...Option,
) *Server {
	s := &Server{
		controllers: apiControllers{
			expenses: newExpensesController(expenses),
			users:    newUsersController(users),
			health:   newHealthController(pinger),
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer)
	r.Use(s.logRequests, s.cors)

	r.Route("/api", func(r chi.Router) {
		s.controllers.expenses.Register(r)
		s.controllers.users.Register(r)
		s.controllers.health.Register(r)
	})

	if s.webhookHandler != nil {
		r.Post(s.webhookPath, s.webhookHandler)
	}

	return r
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		path := r.URL.Path
		if r.URL.RawQuery != "" {
			path += "?" + r.URL.RawQuery
		}
		fmt.Printf("[http] %s %s -> %d (%s)\n", r.Method, path, ww.Status(), time.Since(start).Round(time.Millisecond))
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
