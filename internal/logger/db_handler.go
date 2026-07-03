package logger

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/dhcp-server/dhcp-server/internal/models"
	"github.com/dhcp-server/dhcp-server/internal/store"
	"github.com/google/uuid"
)

// DBHandler wraps a base slog.Handler and additionally persists warn/error
// records to the database so they can be viewed in the web UI.
type DBHandler struct {
	base   slog.Handler
	store  *store.Store
	nodeID string
	mu     sync.Mutex
}

// NewDBHandler creates a handler that writes to w (via the base text/json
// handler) and also stores warn/error records in the database.
func NewDBHandler(store *store.Store, nodeID string, w io.Writer, opts *slog.HandlerOptions) *DBHandler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return &DBHandler{
		base:   slog.NewTextHandler(w, opts),
		store:  store,
		nodeID: nodeID,
	}
}

func (h *DBHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.base.Enabled(ctx, level)
}

func (h *DBHandler) Handle(ctx context.Context, r slog.Record) error {
	// Always output to the base handler first.
	if err := h.base.Handle(ctx, r); err != nil {
		return err
	}

	// Persist warn/error to the database.
	if r.Level >= slog.LevelWarn && h.store != nil {
		h.persist(r)
	}
	return nil
}

func (h *DBHandler) persist(r slog.Record) {
	attrs := make(map[string]interface{})
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	attrsBytes, _ := json.Marshal(attrs)

	log := &models.SystemLog{
		ID:        uuid.New().String(),
		NodeID:    h.nodeID,
		Level:     r.Level.String(),
		Message:   r.Message,
		Attrs:     attrsBytes,
		CreatedAt: r.Time,
	}
	// Use a short timeout so logging never blocks shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = h.store.CreateSystemLog(ctx, log)
}

func (h *DBHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &DBHandler{
		base:   h.base.WithAttrs(attrs),
		store:  h.store,
		nodeID: h.nodeID,
	}
}

func (h *DBHandler) WithGroup(name string) slog.Handler {
	return &DBHandler{
		base:   h.base.WithGroup(name),
		store:  h.store,
		nodeID: h.nodeID,
	}
}
