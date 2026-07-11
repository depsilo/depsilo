package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/middleware"
)

type UserHandler struct {
	db         *gorm.DB
	mutationMu sync.Mutex
	testHooks  *userMutationTestHooks
}

type userMutationTestHooks struct {
	lock                 func()
	unlock               func()
	afterMutationLock    func()
	afterInvariantRead   func()
	beforeMutationUnlock func()
	afterMutationUnlock  func()
}

func NewUserHandler(database *gorm.DB) *UserHandler {
	return &UserHandler{db: database}
}

func (h *UserHandler) usesTestMutationLocker() bool {
	return h.testHooks != nil && h.testHooks.lock != nil && h.testHooks.unlock != nil
}

func (h *UserHandler) lockMutation() {
	if h.usesTestMutationLocker() {
		h.testHooks.lock()
		return
	}
	h.mutationMu.Lock()
}

func (h *UserHandler) signalMutationLocked() {
	if h.testHooks != nil && h.testHooks.afterMutationLock != nil {
		h.testHooks.afterMutationLock()
	}
}

func (h *UserHandler) unlockMutation() {
	if h.testHooks != nil && h.testHooks.afterMutationUnlock != nil {
		defer h.testHooks.afterMutationUnlock()
	}
	if h.usesTestMutationLocker() {
		defer h.testHooks.unlock()
	} else {
		defer h.mutationMu.Unlock()
	}
	if h.testHooks != nil && h.testHooks.beforeMutationUnlock != nil {
		h.testHooks.beforeMutationUnlock()
	}
}

func (h *UserHandler) signalInvariantRead() {
	if h.testHooks != nil && h.testHooks.afterInvariantRead != nil {
		h.testHooks.afterInvariantRead()
	}
}

type userResponse struct {
	ID          uint       `json:"id"`
	Username    string     `json:"username"`
	Role        string     `json:"role"`
	Enabled     bool       `json:"enabled"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func toUserResponse(user db.User) userResponse {
	return userResponse{
		ID:          user.ID,
		Username:    user.Username,
		Role:        user.Role,
		Enabled:     user.Enabled,
		LastLoginAt: user.LastLoginAt,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}
}

func toUserResponses(users []db.User) []userResponse {
	items := make([]userResponse, len(users))
	for i, user := range users {
		items[i] = toUserResponse(user)
	}
	return items
}

func (h *UserHandler) List(c *gin.Context) {
	var users []db.User
	if err := h.db.Order("datetime(created_at)").Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toUserResponses(users))
}

type createUserRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required,min=4"`
	Role     string `json:"role" binding:"required,oneof=admin readonly"`
}

func (h *UserHandler) Create(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBadUserRequest(c, err.Error())
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
		if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			c.JSON(http.StatusConflict, gin.H{"code": "CONFLICT", "message": "username already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, toUserResponse(user))
}

type updateUserRequest struct {
	Password *string `json:"password"`
	Role     *string `json:"role"`
	Enabled  *bool   `json:"enabled"`
}

var (
	errSelfLockout = errors.New("self lockout")
	errLastAdmin   = errors.New("last enabled admin")
	errUserMissing = errors.New("user not found")
)

func parseUserID(value string) (uint, error) {
	id, err := strconv.ParseUint(value, 10, strconv.IntSize)
	if err != nil || id == 0 {
		return 0, errors.New("invalid id")
	}
	return uint(id), nil
}

func validateUpdateUserRequest(req updateUserRequest) error {
	if req.Password == nil && req.Role == nil && req.Enabled == nil {
		return errors.New("at least one update field is required")
	}
	if req.Password != nil && len(*req.Password) < 4 {
		return errors.New("password must be at least 4 characters")
	}
	if req.Role != nil && *req.Role != "admin" && *req.Role != "readonly" {
		return errors.New("role must be admin or readonly")
	}
	return nil
}

func mutationPrincipal(c *gin.Context) (middleware.Principal, error) {
	principal, ok := middleware.PrincipalFromContext(c)
	if !ok {
		return middleware.Principal{}, errors.New("principal unavailable")
	}
	return principal, nil
}

func effectiveAdmin(user db.User) bool {
	return user.Role == "admin" && user.Enabled
}

func anotherEffectiveAdminExists(tx *gorm.DB, excludedID uint) (bool, error) {
	var count int64
	err := tx.Model(&db.User{}).
		Where("role = ? AND enabled = ? AND id != ?", "admin", true, excludedID).
		Count(&count).Error
	return count > 0, err
}

func writeBadUserRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": message})
}

func writeUserMutationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errSelfLockout):
		c.JSON(http.StatusConflict, gin.H{"code": "SELF_LOCKOUT", "message": "current user cannot delete, disable, or demote itself"})
	case errors.Is(err, errLastAdmin):
		c.JSON(http.StatusConflict, gin.H{"code": "LAST_ADMIN", "message": "at least one enabled admin must remain"})
	case errors.Is(err, errUserMissing):
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "user not found"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
	}
}

func (h *UserHandler) Update(c *gin.Context) {
	id, err := parseUserID(c.Param("id"))
	if err != nil {
		writeBadUserRequest(c, "invalid id")
		return
	}

	var req updateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBadUserRequest(c, err.Error())
		return
	}
	if err := validateUpdateUserRequest(req); err != nil {
		writeBadUserRequest(c, err.Error())
		return
	}

	principal, err := mutationPrincipal(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": err.Error()})
		return
	}

	updates := map[string]any{}
	if req.Password != nil {
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

	h.lockMutation()
	defer h.unlockMutation()
	h.signalMutationLocked()

	var saved db.User
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var target db.User
		if err := tx.First(&target, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errUserMissing
			}
			return err
		}

		selfDisable := req.Enabled != nil && !*req.Enabled
		selfDemote := req.Role != nil && *req.Role != "admin"
		if target.ID == principal.ID && (selfDisable || selfDemote) {
			return errSelfLockout
		}
		if effectiveAdmin(target) && (selfDisable || selfDemote) {
			another, err := anotherEffectiveAdminExists(tx, target.ID)
			if err != nil {
				return err
			}
			if !another {
				return errLastAdmin
			}
			h.signalInvariantRead()
		}

		if err := tx.Model(&target).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&saved, target.ID).Error
	})
	if err != nil {
		writeUserMutationError(c, err)
		return
	}
	c.JSON(http.StatusOK, toUserResponse(saved))
}

func (h *UserHandler) Delete(c *gin.Context) {
	id, err := parseUserID(c.Param("id"))
	if err != nil {
		writeBadUserRequest(c, "invalid id")
		return
	}
	principal, err := mutationPrincipal(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": err.Error()})
		return
	}
	if id == principal.ID {
		c.JSON(http.StatusConflict, gin.H{"code": "SELF_LOCKOUT", "message": "current user cannot delete itself"})
		return
	}

	h.lockMutation()
	defer h.unlockMutation()
	h.signalMutationLocked()

	err = h.db.Transaction(func(tx *gorm.DB) error {
		var target db.User
		if err := tx.First(&target, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errUserMissing
			}
			return err
		}
		if effectiveAdmin(target) {
			another, err := anotherEffectiveAdminExists(tx, target.ID)
			if err != nil {
				return err
			}
			if !another {
				return errLastAdmin
			}
			h.signalInvariantRead()
		}
		if err := tx.Where("user_id = ?", target.ID).Delete(&db.APIToken{}).Error; err != nil {
			return err
		}
		return tx.Delete(&target).Error
	})
	if err != nil {
		writeUserMutationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}
