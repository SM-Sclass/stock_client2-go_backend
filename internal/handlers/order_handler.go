package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/repository"
	"github.com/gin-gonic/gin"
)


type OrderHandler struct {
	OrderRepo *repository.OrderRepository
}

func (h *OrderHandler) GetAllOrders(c *gin.Context) {
	orders, err := h.OrderRepo.GetAllOrders(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to get orders", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"orders": orders})
}

func (h *OrderHandler) GetStockOrders(c *gin.Context) {
	idParam := c.Param("id")
	pageNumber, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	var id int64
	_, err := fmt.Sscan(idParam, &id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id parameter"})
		return
	}
	orders, err := h.OrderRepo.GetOrdersByTrackingStockID(c.Request.Context(), id, pageNumber, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to get orders", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"orders": orders})
}