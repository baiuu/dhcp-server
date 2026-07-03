package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dhcp-server/dhcp-server/internal/auth"
	"github.com/dhcp-server/dhcp-server/internal/metrics"
	"github.com/dhcp-server/dhcp-server/internal/models"
)

func (a *API) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}
		header := r.Header.Get("Authorization")
		if header == "" {
			a.jsonError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			a.jsonError(w, http.StatusUnauthorized, "invalid authorization header")
			return
		}
		user, err := a.auth.ValidateToken(parts[1])
		if err != nil {
			a.jsonError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		ctx := context.WithValue(r.Context(), auth.ContextUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *API) adminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Context().Value(auth.ContextUserKey).(*models.User)
		if user.Role != "admin" {
			a.jsonError(w, http.StatusForbidden, "admin required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) readWriteOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}
		user := r.Context().Value(auth.ContextUserKey).(*models.User)
		if user.Role == "readonly" && r.Method != "GET" {
			a.jsonError(w, http.StatusForbidden, "readonly user cannot modify resources")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rec, r)
		duration := time.Since(start).Seconds()
		path := r.URL.Path
		metrics.HTTPRequestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(rec.statusCode)).Inc()
		metrics.HTTPRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.statusCode = code
	rec.ResponseWriter.WriteHeader(code)
}
