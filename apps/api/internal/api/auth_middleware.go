package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type internalSessionResponse struct {
	Session *struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	} `json:"session"`
}

func (s *Server) attachUserFromAuthService(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AuthBaseURL == "" {
			next.ServeHTTP(w, r)
			return
		}

		u, err := url.Parse(s.cfg.AuthBaseURL)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		u.Path = "/internal/session"

		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u.String(), nil)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		// Forward cookies for session lookup.
		if cookie := r.Header.Get("Cookie"); cookie != "" {
			req.Header.Set("Cookie", cookie)
		}

		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err == nil && resp != nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var out internalSessionResponse
				if err := json.NewDecoder(resp.Body).Decode(&out); err == nil && out.Session != nil {
					userID := strings.TrimSpace(out.Session.User.ID)
					email := strings.TrimSpace(out.Session.User.Email)
					if userID != "" {
						r.Header.Set("X-User-Id", userID)
					}
					if email != "" {
						r.Header.Set("X-User-Email", email)
					}
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}
