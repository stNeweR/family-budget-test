package restapi

import "net/http"

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.services.Family.ListUsers(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}
