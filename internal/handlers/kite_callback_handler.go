package handlers

import (
	"log"
	"net/http"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/app"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/kite"
	"github.com/gin-gonic/gin"
)

type KiteCallbackHandler struct {
	Kc      *kite.KiteClient
	Runtime *app.Runtime
}

func (h *KiteCallbackHandler) KiteCallback(c *gin.Context) {
	requestToken := c.Query("request_token")
	if requestToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing request_token"})
		return
	}

	isValid := h.Kc.IsTokenValid()
	if isValid {
		c.JSON(http.StatusOK, gin.H{"message": "already authenticated"})
		return
	}

	err := h.Kc.GenerateSession(requestToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate session"})
		return
	}

	if err := app.StartKiteRuntime(h.Runtime); err != nil {
		log.Printf("⚠️ runtime start failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "kite authenticated but runtime failed to start"})
		return
	}

	// Load tracked stocks after successful auth
	if err := app.LoadTrackedStocksOnStartup(h.Runtime); err != nil {
		log.Printf("⚠️ Failed to load tracked stocks: %v", err)
	}

	// Start engines if market is currently open
	app.StartEnginesIfMarketOpen(h.Runtime)

	c.JSON(200, gin.H{"message": "kite authenticated, services starting"})
}
