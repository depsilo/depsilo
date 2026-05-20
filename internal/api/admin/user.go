package admin

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

type UserHandler struct {
	db *gorm.DB
}

func NewUserHandler(database *gorm.DB) *UserHandler {
	return &UserHandler{db: database}
}

func (h *UserHandler) List(c *gin.Context) {
	var users []db.User
	h.db.Select("id, username, role, enabled, last_login_at, created_at, updated_at").
		Order("datetime(created_at)").Find(&users)
	c.JSON(http.StatusOK, users)
}

type createUserRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required,min=4"`
	Role     string `json:"role" binding:"required,oneof=admin readonly"`
}

func (h *UserHandler) Create(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "failed to hash password"})
		return
	}

	user := db.User{
		Username:     req.Username,
		PasswordHash: string(hash),
		Role:         req.Role,
		Enabled:      true,
	}

	if err := h.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": "CONFLICT", "message": "username already exists"})
		return
	}

	user.PasswordHash = "" // don't return hash
	c.JSON(http.StatusCreated, user)
}

type updateUserRequest struct {
	Password *string `json:"password"`
	Role     *string `json:"role" binding:"omitempty,oneof=admin readonly"`
	Enabled  *bool   `json:"enabled"`
}

func (h *UserHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	var user db.User
	if err := h.db.First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "user not found"})
		return
	}

	var req updateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}

	updates := map[string]interface{}{}
	if req.Password != nil && *req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "failed to hash password"})
			return
		}
		updates["password_hash"] = string(hash)
	}
	if req.Role != nil {
		updates["role"] = *req.Role
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if len(updates) > 0 {
		h.db.Model(&user).Updates(updates)
	}

	h.db.First(&user, id)
	user.PasswordHash = ""
	c.JSON(http.StatusOK, user)
}

func (h *UserHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	// Prevent deleting the last admin
	var adminCount int64
	h.db.Model(&db.User{}).Where("role = ? AND id != ?", "admin", id).Count(&adminCount)
	if adminCount == 0 {
		var user db.User
		h.db.First(&user, id)
		if user.Role == "admin" {
			c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "cannot delete the last admin"})
			return
		}
	}

	result := h.db.Delete(&db.User{}, id)
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "user not found"})
		return
	}

	// Also delete user's tokens
	h.db.Where("user_id = ?", id).Delete(&db.APIToken{})

	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}
