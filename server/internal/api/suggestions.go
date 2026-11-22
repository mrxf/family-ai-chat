package api

import "net/http"

func (s *Server) getHomeSuggestions(w http.ResponseWriter, r *http.Request) {
	res, err := s.Chat.GetHomeSuggestions(r.Context(), currentUser(r))
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, res)
}
