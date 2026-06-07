package handlers

import (
	"log"
	"net/http"
	"time"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/models"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/config"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/repository"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/services"
	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	UserRepo *repository.UserRepository
}

type LoginRequest struct {
	Phone    string `json:"phone" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type SignupRequest struct {
	FullName string `json:"full_name" binding:"required"`
	Phone    string `json:"phone" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type ProfileResponse struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	Phone    string `json:"phone"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.UserRepo.GetByPhone(c.Request.Context(), req.Phone)
	if err != nil {
		log.Printf("Error fetching user: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	if err := services.CheckPassword(user.Password, req.Password); err != nil {
		log.Printf("Error checking password: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	token, err := services.GenerateToken(user.ID)
	if err != nil {
		log.Printf("Error generating token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token generation failed"})
		return
	}

	isProduction := config.ServerConfig.GoEnv == "production"
	sameSiteMode := http.SameSiteNoneMode
	if isProduction {
		sameSiteMode = http.SameSiteLaxMode
	}

	c.SetCookieData(&http.Cookie{
		Name:     "access_token",
		Value:    token,
		Path:     "/",
		Domain:   config.ServerConfig.CookieDomain,
		Expires:  time.Now().Add(24 * time.Hour),
		MaxAge:   86400,
		Secure:   isProduction,
		HttpOnly: true,
		SameSite: sameSiteMode,
		// Partitioned: true, // Go 1.22+
	})

	c.JSON(http.StatusOK, gin.H{
		"token": token,
	})
}

func (h *AuthHandler) Signup(c *gin.Context) {
	var req SignupRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hashedPassword, err := services.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "password hashing failed"})
		return
	}

	newUser := &models.User{
		FullName:  req.FullName,
		Phone:     req.Phone,
		Password:  hashedPassword,
		CreatedAt: time.Now(),
	}

	err = h.UserRepo.Create(c.Request.Context(), newUser)
	if err != nil {
		log.Printf("Error creating user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user creation failed"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "user created successfully",
		"user":    newUser,
	})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	isProduction := config.ServerConfig.GoEnv == "production"
	sameSiteMode := http.SameSiteNoneMode
	if isProduction {
		sameSiteMode = http.SameSiteLaxMode
	}

	c.SetCookieData(&http.Cookie{
		Name:     "access_token",
		Value:    "",
		Path:     "/",
		Domain:   config.ServerConfig.CookieDomain,
		Expires:  time.Now().Add(-1 * time.Hour),
		MaxAge:   -1,
		Secure:   isProduction,
		HttpOnly: true,
		SameSite: sameSiteMode,
	})

	c.JSON(http.StatusOK, gin.H{
		"message": "logged out successfully",
	})
}

func (h *AuthHandler) Profile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user ID not found in context"})
		return
	}

	user, err := h.UserRepo.GetUserProfile(c.Request.Context(), int64(userID.(float64)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve user"})
		return
	}

	userProfile := ProfileResponse{
		ID:       user.ID,
		FullName: user.FullName,
		Phone:    user.Phone,
	}

	c.JSON(http.StatusOK, gin.H{
		"user": userProfile,
	})
}
