package handlers

import (
	"net/http"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/services"
	"github.com/gin-gonic/gin"
)

type StockQueryHandler struct {
	InstrumentService *services.InstrumentService
}

func (h *StockQueryHandler) GetSearchedStock(c *gin.Context) {
	instList, err := h.InstrumentService.GetSearchedInstrumentByName(c.Query("name"))
	if err != nil {
		if err.Error() == "instrument not found for name: "+c.Query("name") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"instruments": instList})
}
