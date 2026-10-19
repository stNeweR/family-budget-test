package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/stNeweR/family-budget/internal/domain"
)

// headKey is the context key for the authenticated family head.
type headKey struct{}

// auth handles the optional Bearer panel token. A request without an
// Authorization header is served anonymously (the task requires an
// unauthenticated panel); a stale or unknown token is rejected with 401 so a
// broken link never silently downgrades to the global view. A valid token
// stores the family head in the request context and scopes all handlers.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			next.ServeHTTP(w, r)
			return
		}
		token := bearerToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "invalid Authorization header")
			return
		}
		head, err := s.store.GetHeadByPanelToken(r.Context(), token)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusUnauthorized, "invalid panel token")
				return
			}
			s.log.Error("panel token lookup", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), headKey{}, head)))
	})
}

// headFrom returns the family head stored by auth; ok is false for anonymous
// requests.
func headFrom(ctx context.Context) (head domain.User, ok bool) {
	head, ok = ctx.Value(headKey{}).(domain.User)
	return head, ok
}

// bearerToken extracts the token from an "Authorization: Bearer ..." header.
func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

// canAccessExpense reports whether the head may see the user's expenses.
func (s *Server) canAccessExpense(ctx context.Context, head domain.User, userID int64) (bool, error) {
	if userID == head.UserID {
		return true, nil
	}
	return s.store.IsUserInFamily(ctx, head.UserID, userID)
}

// loadAccessibleExpense fetches the expense and writes a 404 unless it belongs
// to the head's family. Anonymous requests may access any expense. It returns
// ok=false after writing the response.
func (s *Server) loadAccessibleExpense(w http.ResponseWriter, r *http.Request, id uuid.UUID) (domain.Expense, bool) {
	expense, err := s.store.GetExpense(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return domain.Expense{}, false
	}
	head, scoped := headFrom(r.Context())
	if !scoped {
		return expense, true
	}
	ok, err := s.canAccessExpense(r.Context(), head, expense.UserID)
	if err != nil {
		writeStoreError(w, err)
		return domain.Expense{}, false
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return domain.Expense{}, false
	}
	return expense, true
}
