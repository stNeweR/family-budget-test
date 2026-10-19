package restapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/stNeweR/family-budget/internal/repo"
	"github.com/stNeweR/family-budget/internal/usecase"
)

type Services struct {
	Expenses usecase.Expense
	Family   usecase.Family
	Pinger   repo.Pinger
}

type Server struct {
	services       Services
	log            *slog.Logger
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

func New(services Services, log *slog.Logger, opts ...Option) *Server {
	s := &Server{services: services, log: log}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(s.logRequests)
	r.Use(s.cors)

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", s.health)
		r.Get("/users", s.listUsers)
		r.Route("/expenses", func(r chi.Router) {
			r.Get("/", s.listExpenses)
			r.Post("/", s.createExpense)
			r.Get("/{id}", s.getExpense)
			r.Patch("/{id}", s.updateExpense)
			r.Delete("/{id}", s.deleteExpense)
		})
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
