package handlers

import (
	"github.com/SM-Sclass/stock_client2-go_backend/internal/services"
	"github.com/gin-gonic/gin"
)

type StockQueryHandler struct {
	InstrumentService *services.InstrumentService
}

func (h *StockQueryHandler) GetSearchedStock(c *gin.Context) {
	instList, err := h.InstrumentService.GetSearchedInstrumentByName(c.Query("name"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"instruments": instList})
}