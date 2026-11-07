// Package handler only parses requests and writes responses; all business
// rules live in the service layer.
package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Pinger is satisfied by *pgxpool.Pool.
type Pinger interface {
	Ping(ctx context.Context) error
}

type HealthHandler struct {
	db Pinger
}

func NewHealthHandler(db Pinger) *HealthHandler {
	return &HealthHandler{db: db}
}

type healthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
	Time     string `json:"time"`
}

// Healthz reports service and database health. Used by Render's health check
// and keep-alive pings.
//
//	@Summary	Health check
//	@Tags		system
//	@Produce	json
//	@Success	200	{object}	healthResponse
//	@Failure	503	{object}	healthResponse
//	@Router		/healthz [get]
func (h *HealthHandler) Healthz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	resp := healthResponse{Status: "ok", Database: "ok", Time: time.Now().UTC().Format(time.RFC3339)}
	status := http.StatusOK
	if err := h.db.Ping(ctx); err != nil {
		resp.Status, resp.Database = "degraded", "unreachable"
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, resp)
}
