package restapi

import "net/http"

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.services.Pinger.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
