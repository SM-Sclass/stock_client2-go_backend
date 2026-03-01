package handlers

import (
	"net/http"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/app"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/kite"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/services"
	"github.com/gin-gonic/gin"
)

type SystemHandler struct {
	InstrumentService *services.InstrumentService
	Kc                *kite.KiteClient
	Runtime           *app.Runtime
}

type SystemStatusResponse struct {
	KiteAuthenticated bool `json:"kite_authenticated"`
	TotalInstruments  int  `json:"total_instruments"`
	IsRuntimeReady    bool `json:"is_runtime_ready"`
}

func (h *SystemHandler) SystemStatus(c *gin.Context) {
	isKiteAuth := h.Kc.IsTokenValid()
	status := SystemStatusResponse{
		KiteAuthenticated: isKiteAuth,
		TotalInstruments:  len(h.InstrumentService.NSEInstruments),
		IsRuntimeReady:    h.Runtime.KiteReady,
	}

	c.JSON(http.StatusOK, gin.H{"status": status})
}
