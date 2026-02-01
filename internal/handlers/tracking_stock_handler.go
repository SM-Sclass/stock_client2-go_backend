package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/app"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/models"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/repository"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/tracking"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/utils"
	"github.com/zerodha/gokiteconnect/v4"
)

type TrackingStockHandler struct {
	TrackingStockRepo *repository.TrackingStocksRepository
	Runtime           *app.Runtime
}

type NewStock struct {
	StockSymbol     string  `json:"stock_symbol" binding:"required"`
	Exchange        string  `json:"exchange" binding:"required"`
	InstrumentToken int64   `json:"instrument_token" binding:"required"`
	Target          float64 `json:"target" binding:"required"`
	StopLoss        float64 `json:"stoploss" binding:"required"`
	Quantity        uint32  `json:"quantity" binding:"required"`
	Status          string  `json:"status" binding:"required"`
}

type StockStatus struct {
	Status string `json:"status" binding:"required"`
}

func (h *TrackingStockHandler) Add(c *gin.Context) {
	var req NewStock
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existingStock, err := h.TrackingStockRepo.GetTrackingStockByTradingSymbol(c.Request.Context(), req.StockSymbol)
	if existingStock != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "tracking stock already exists"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to check existing tracking stock", "error": err.Error()})
		return
	}

	newTrackingStock := &models.TrackingStock{
		StockSymbol:     req.StockSymbol,
		Exchange:        req.Exchange,
		InstrumentToken: req.InstrumentToken,
		Target:          req.Target,
		StopLoss:        req.StopLoss,
		Quantity:        req.Quantity,
		Status:          req.Status,
	}

	var marketOpen = utils.IsTradingDay()

	if !marketOpen {
		if newTrackingStock.Status == "ACTIVE" {
			newTrackingStock.Status = "AUTO_INACTIVE"
		}
	}

	ID, err := h.TrackingStockRepo.AddTrackingStock(c.Request.Context(), newTrackingStock)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to add tracking stock", "error": err.Error()})
		return
	}

	if marketOpen && newTrackingStock.Status == "ACTIVE" && h.Runtime.KiteReady {
		trackingStock := tracking.TrackedStock{
			StockSymbol:     newTrackingStock.StockSymbol,
			InstrumentToken: uint32(newTrackingStock.InstrumentToken),
			Target:          newTrackingStock.Target,
			StopLoss:        newTrackingStock.StopLoss,
			Quantity:        newTrackingStock.Quantity,
			Exchange:        newTrackingStock.Exchange,
		}
		
		baseLTP, err := h.Runtime.KiteClient.KiteConnect.GetLTP(newTrackingStock.StockSymbol)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to get LTP for tracking stock", "error": err.Error()})
			return
		}

		trackingStock.BasePrice = baseLTP[newTrackingStock.StockSymbol].LastPrice
		h.Runtime.TrackingManager.AddTrackingStock(trackingStock)
	}

	c.JSON(http.StatusOK, gin.H{"id": ID})
}

func (h *TrackingStockHandler) GetAll(c *gin.Context) {
	trackingStocks, err := h.TrackingStockRepo.GetAllTrackingStocks(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to retrieve tracking stocks", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, trackingStocks)
}

func (h *TrackingStockHandler) GetDetail(c *gin.Context) {
	idParam := c.Param("id")
	var id int64
	_, err := fmt.Sscan(idParam, &id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id parameter"})
		return
	}

	trackingStock, err := h.TrackingStockRepo.GetTrackingStockByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to retrieve tracking stock", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, trackingStock)
}

func (h *TrackingStockHandler) Update(c *gin.Context) {
	var req models.TrackingStock
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	idParam := c.Param("id")
	var id int64
	_, err := fmt.Sscan(idParam, &id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id parameter"})
		return
	}

	err = h.TrackingStockRepo.UpdateTrackingStock(c.Request.Context(), &req, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to update tracking stock", "error": err.Error()})
		return
	}

	if !utils.IsTradingDay() {
		c.JSON(http.StatusOK, gin.H{"message": "tracking stock updated successfully"})
		return
	}

	if h.Runtime.KiteReady {
		trackingStockData, err := h.TrackingStockRepo.GetTrackingStockByID(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to retrieve tracking stock", "error": err.Error()})
			return
		}

		if trackingStockData.Status == "ACTIVE" {
			trackingStock := tracking.TrackedStock{
				StockSymbol:     trackingStockData.StockSymbol,
				InstrumentToken: uint32(trackingStockData.InstrumentToken),
				Target:          trackingStockData.Target,
				StopLoss:        trackingStockData.StopLoss,
				Quantity:        trackingStockData.Quantity, // ← ADD THESE
				Exchange:        trackingStockData.Exchange,
			}
			h.Runtime.TrackingManager.UpdateStockParameters(trackingStock)
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "tracking stock updated successfully"})
}

func (h *TrackingStockHandler) UpdateStatus(c *gin.Context) {
	idParam := c.Param("id")
	var id int64
	_, err := fmt.Sscan(idParam, &id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id parameter"})
		return
	}

	var req StockStatus
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err = h.TrackingStockRepo.UpdateTrackingStockStatus(c.Request.Context(), id, req.Status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to update tracking stock status", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "tracking stock status updated successfully"})
}

func (h *TrackingStockHandler) UpdateStatusToStart(c *gin.Context) {
	idParam := c.Param("id")
	var id int64
	_, err := fmt.Sscan(idParam, &id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id parameter"})
		return
	}

	if !utils.IsTradingDay() {
		c.JSON(http.StatusBadRequest, gin.H{"message": "market is closed, cannot start tracking stock"})
		return
	}

	req := StockStatus{
		Status: "ACTIVE",
	}

	trackingStockData, err := h.TrackingStockRepo.GetTrackingStockByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to retrieve tracking stock", "error": err.Error()})
		return
	}

	if h.Runtime.KiteReady {
		var trackingStock tracking.TrackedStock
		trackingStock.StockSymbol = trackingStockData.StockSymbol
		trackingStock.InstrumentToken = uint32(trackingStockData.InstrumentToken)
		trackingStock.Target = trackingStockData.Target
		trackingStock.StopLoss = trackingStockData.StopLoss
		var baseLTP kiteconnect.QuoteLTP
		baseLTP, err = h.Runtime.KiteClient.KiteConnect.GetLTP(trackingStock.StockSymbol)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to get LTP for tracking stock", "error": err.Error()})
			return
		}

		trackingStock.BasePrice = baseLTP[trackingStockData.StockSymbol].LastPrice
		h.Runtime.TrackingManager.AddTrackingStock(trackingStock)
	}

	err = h.TrackingStockRepo.UpdateTrackingStockStatus(c.Request.Context(), id, req.Status)
	if err != nil {
		// h.UpdateStatusToStop()
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to update tracking stock status", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "tracking stock started successfully"})
}

func (h *TrackingStockHandler) UpdateStatusToStop(c *gin.Context) {
	idParam := c.Param("id")
	var id int64
	_, err := fmt.Sscan(idParam, &id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id parameter"})
		return
	}

	req := StockStatus{
		Status: "INACTIVE",
	}

	err = h.TrackingStockRepo.UpdateTrackingStockStatus(c.Request.Context(), id, req.Status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to update tracking stock status", "error": err.Error()})
		return
	}

	if !utils.IsTradingDay() {
		c.JSON(http.StatusOK, gin.H{"message": "tracking stock stopped successfully"})
		return
	}

	if h.Runtime.KiteReady {
		trackingStockData, err := h.TrackingStockRepo.GetTrackingStockByID(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to retrieve tracking stock", "error": err.Error()})
			return
		}

		h.Runtime.TrackingManager.RemoveStockFromTracking(uint32(trackingStockData.InstrumentToken))
	}

	c.JSON(http.StatusOK, gin.H{"message": "tracking stock stopped successfully"})
}

func (h *TrackingStockHandler) Delete(c *gin.Context) {
	idParam := c.Param("id")
	var id int64
	_, err := fmt.Sscan(idParam, &id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id parameter"})
		return
	}

	err = h.TrackingStockRepo.DeleteTrackingStock(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to delete tracking stock", "error": err.Error()})
		return
	}

	if !utils.IsTradingDay() {
		c.JSON(http.StatusOK, gin.H{"message": "tracking stock deleted successfully"})
		return
	}

	if h.Runtime.KiteReady {
		trackingStockData, err := h.TrackingStockRepo.GetTrackingStockByID(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to retrieve tracking stock", "error": err.Error()})
			return
		}

		h.Runtime.TrackingManager.RemoveStockFromTracking(uint32(trackingStockData.InstrumentToken))
	}

	c.JSON(http.StatusOK, gin.H{"message": "tracking stock deleted successfully"})
}
