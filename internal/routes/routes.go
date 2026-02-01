package routes

import (
	"github.com/SM-Sclass/stock_client2-go_backend/internal/handlers"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterRoutes(
	router *gin.Engine,
	authHandler *handlers.AuthHandler,
	trackingStockHandler *handlers.TrackingStockHandler,
	kiteCallbackHandler *handlers.KiteCallbackHandler,
	orderHandler *handlers.OrderHandler,
	stockQueryHandler *handlers.StockQueryHandler,
) {
	api := router.Group("/api/v1")

	api.POST("/login", authHandler.Login)
	api.POST("/signup", authHandler.Signup)

	protected := api.Group("/")
	protected.Use(middleware.AuthMiddleware())

	// Tracking Stocks Routes
	protected.POST("/tracking-stocks", trackingStockHandler.Add)
	protected.GET("/tracking-stocks", trackingStockHandler.GetAll)
	protected.GET("/tracking-stocks/:id", trackingStockHandler.GetDetail)
	protected.PUT("/tracking-stocks/:id", trackingStockHandler.Update)
	protected.DELETE("/tracking-stocks/:id", trackingStockHandler.Delete)
	protected.PATCH("/tracking-stocks/:id/start", trackingStockHandler.UpdateStatusToStart)
	protected.PATCH("/tracking-stocks/:id/stop", trackingStockHandler.UpdateStatusToStop)

	// Kite Callback Route
	router.GET("/kite/callback", kiteCallbackHandler.KiteCallback)

	// Order Routes
	protected.GET("/orders", orderHandler.GetAllOrders)
	protected.GET("/tracking-stocks/:id/orders", orderHandler.GetStockOrders)

	// Stock Query Route
	protected.GET("/stocks/search", stockQueryHandler.GetSearchedStock)

	protected.GET("/profile", func(c *gin.Context) {
		userID, _ := c.Get("user_id")
		c.JSON(200, gin.H{"user_id": userID})
	})
}
