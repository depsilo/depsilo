package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
	"depsilo/internal/middleware"
)

func newUserTestRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	r, database, _ := newUserTestRouterWithHandler(t)
	return r, database
}

func newUserTestRouterWithHandler(t *testing.T) (*gin.Engine, *gorm.DB, *UserHandler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "users.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.User{}, &db.APIToken{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h := NewUserHandler(database)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		actorID, err := strconv.ParseUint(c.GetHeader("X-Actor-ID"), 10, 64)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(middleware.ContextKeyPrincipal, middleware.Principal{
			ID: uint(actorID), Username: "actor", Role: "admin", Enabled: true,
			AuthMethod: middleware.AuthMethodJWT, CanWrite: true,
		})
		c.Next()
	})
	r.GET("/users", h.List)
	r.POST("/users", h.Create)
	r.PUT("/users/:id", h.Update)
	r.DELETE("/users/:id", h.Delete)
	return r, database, h
}

func createUserForMutationTest(t *testing.T, database *gorm.DB, username, role string, enabled bool) db.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	user := db.User{Username: username, PasswordHash: string(hash), Role: role, Enabled: enabled}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if !enabled {
		if err := database.Model(&user).Update("enabled", false).Error; err != nil {
			t.Fatalf("disable user: %v", err)
		}
		user.Enabled = false
	}
	return user
}

func userMutationRequest(r *gin.Engine, method, path, body string, actorID uint) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Actor-ID", strconv.FormatUint(uint64(actorID), 10))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func responseCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	return body.Code
}

func assertSafeUserObject(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode user object: %v; body=%s", err, raw)
	}
	allowed := map[string]bool{
		"id": true, "username": true, "role": true, "enabled": true,
		"last_login_at": true, "created_at": true, "updated_at": true,
	}
	if len(object) != len(allowed) {
		t.Fatalf("user response keys = %#v", object)
	}
	for key := range object {
		if !allowed[key] {
			t.Fatalf("user response leaked noncontract field %q: %s", key, raw)
		}
	}
	for key := range allowed {
		if _, ok := object[key]; !ok {
			t.Fatalf("user response missing %q: %s", key, raw)
		}
	}
}

func injectUserGORMFailure(t *testing.T, database *gorm.DB, operation, table string, occurrence int) {
	t.Helper()
	callbackName := "test:user_" + operation + "_failure"
	seen := 0
	callback := func(tx *gorm.DB) {
		if tx.Statement.Table != table {
			return
		}
		seen++
		if seen == occurrence {
			tx.AddError(errors.New("injected user database failure"))
		}
	}
	switch operation {
	case "query":
		if err := database.Callback().Query().Before("gorm:query").Register(callbackName, callback); err != nil {
			t.Fatalf("register query callback: %v", err)
		}
		t.Cleanup(func() { _ = database.Callback().Query().Remove(callbackName) })
	case "update":
		if err := database.Callback().Update().Before("gorm:update").Register(callbackName, callback); err != nil {
			t.Fatalf("register update callback: %v", err)
		}
		t.Cleanup(func() { _ = database.Callback().Update().Remove(callbackName) })
	case "delete":
		if err := database.Callback().Delete().Before("gorm:delete").Register(callbackName, callback); err != nil {
			t.Fatalf("register delete callback: %v", err)
		}
		t.Cleanup(func() { _ = database.Callback().Delete().Remove(callbackName) })
	case "create":
		if err := database.Callback().Create().Before("gorm:create").Register(callbackName, callback); err != nil {
			t.Fatalf("register create callback: %v", err)
		}
		t.Cleanup(func() { _ = database.Callback().Create().Remove(callbackName) })
	default:
		t.Fatalf("unsupported operation %q", operation)
	}
}

func TestUserListCreateAndUpdateUseSafeDTO(t *testing.T) {
	r, database := newUserTestRouter(t)
	actor := createUserForMutationTest(t, database, "owner", "admin", true)
	target := createUserForMutationTest(t, database, "reader", "readonly", true)

	var _ userResponse = toUserResponse(target)

	listRec := userMutationRequest(r, http.MethodGet, "/users", "", actor.ID)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var list []json.RawMessage
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil || len(list) != 2 {
		t.Fatalf("decode list: err=%v body=%s", err, listRec.Body.String())
	}
	for _, item := range list {
		assertSafeUserObject(t, item)
	}

	createRec := userMutationRequest(r, http.MethodPost, "/users", `{"username":"new-reader","password":"create-secret","role":"readonly"}`, actor.ID)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	assertSafeUserObject(t, createRec.Body.Bytes())

	path := "/users/" + strconv.FormatUint(uint64(target.ID), 10)
	updateRec := userMutationRequest(r, http.MethodPut, path, `{"password":"update-secret"}`, actor.ID)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}
	assertSafeUserObject(t, updateRec.Body.Bytes())
}

func TestUserCannotLockOutSelfButCanChangePassword(t *testing.T) {
	r, database := newUserTestRouter(t)
	admin := createUserForMutationTest(t, database, "admin", "admin", true)
	path := "/users/" + strconv.FormatUint(uint64(admin.ID), 10)
	for _, body := range []string{`{"role":"readonly"}`, `{"enabled":false}`} {
		rec := userMutationRequest(r, http.MethodPut, path, body, admin.ID)
		if rec.Code != http.StatusConflict || responseCode(t, rec) != "SELF_LOCKOUT" {
			t.Fatalf("body %s status=%d response=%s", body, rec.Code, rec.Body.String())
		}
	}
	deleteRec := userMutationRequest(r, http.MethodDelete, path, "", admin.ID)
	if deleteRec.Code != http.StatusConflict || responseCode(t, deleteRec) != "SELF_LOCKOUT" {
		t.Fatalf("self delete status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	passwordRec := userMutationRequest(r, http.MethodPut, path, `{"password":"new-password"}`, admin.ID)
	if passwordRec.Code != http.StatusOK {
		t.Fatalf("password status=%d body=%s", passwordRec.Code, passwordRec.Body.String())
	}
	var saved db.User
	if err := database.First(&saved, admin.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(saved.PasswordHash), []byte("new-password")); err != nil {
		t.Fatalf("password not updated: %v", err)
	}
	if saved.Role != "admin" || !saved.Enabled {
		t.Fatalf("saved user = %#v", saved)
	}
}

func TestUserMutationsPreserveEffectiveAdmin(t *testing.T) {
	tests := []struct {
		name   string
		method string
		body   string
	}{
		{name: "demote", method: http.MethodPut, body: `{"role":"readonly"}`},
		{name: "disable", method: http.MethodPut, body: `{"enabled":false}`},
		{name: "delete", method: http.MethodDelete},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, database := newUserTestRouter(t)
			actor := createUserForMutationTest(t, database, "actor", "readonly", true)
			target := createUserForMutationTest(t, database, "only-admin", "admin", true)
			createUserForMutationTest(t, database, "disabled-admin", "admin", false)
			path := "/users/" + strconv.FormatUint(uint64(target.ID), 10)
			rec := userMutationRequest(r, tc.method, path, tc.body, actor.ID)
			if rec.Code != http.StatusConflict || responseCode(t, rec) != "LAST_ADMIN" {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

type concurrentUserMutation struct {
	method string
	body   string
	actor  func(first, second db.User) uint
	target func(first, second db.User) uint
}

func runDeterministicAdminRace(t *testing.T, firstMutation, secondMutation concurrentUserMutation) {
	t.Helper()
	r, database, handler := newUserTestRouterWithHandler(t)
	first := createUserForMutationTest(t, database, "first", "admin", true)
	second := createUserForMutationTest(t, database, "second", "admin", true)

	lockToken := make(chan struct{}, 1)
	lockToken <- struct{}{}
	lockAttempts := make(chan int32, 2)
	firstInvariantRead := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstReleased := false
	defer func() {
		if !firstReleased {
			close(releaseFirst)
		}
	}()
	var lockAttemptCount atomic.Int32
	var lockAcquisitions atomic.Int32
	var lockReleases atomic.Int32
	var validatedMutations atomic.Int32
	var guardedEntries atomic.Int32
	var guardedExits atomic.Int32
	var currentGuarded atomic.Int32
	var maximumGuarded atomic.Int32
	handler.testHooks = &userMutationTestHooks{
		lock: func() {
			attempt := lockAttemptCount.Add(1)
			lockAttempts <- attempt
			<-lockToken
			lockAcquisitions.Add(1)
		},
		unlock: func() {
			lockReleases.Add(1)
			lockToken <- struct{}{}
		},
		afterMutationLock: func() {
			guardedEntries.Add(1)
			current := currentGuarded.Add(1)
			for maximum := maximumGuarded.Load(); current > maximum; maximum = maximumGuarded.Load() {
				if maximumGuarded.CompareAndSwap(maximum, current) {
					break
				}
			}
		},
		afterInvariantRead: func() {
			if validatedMutations.Add(1) == 1 {
				close(firstInvariantRead)
				<-releaseFirst
			}
		},
		beforeMutationUnlock: func() {
			currentGuarded.Add(-1)
		},
		afterMutationUnlock: func() {
			guardedExits.Add(1)
		},
	}

	run := func(mutation concurrentUserMutation) *httptest.ResponseRecorder {
		actorID := mutation.actor(first, second)
		targetID := mutation.target(first, second)
		path := "/users/" + strconv.FormatUint(uint64(targetID), 10)
		return userMutationRequest(r, mutation.method, path, mutation.body, actorID)
	}
	firstResult := make(chan *httptest.ResponseRecorder, 1)
	secondResult := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstResult <- run(firstMutation) }()
	waitForUserLockAttempt(t, lockAttempts, 1)
	waitForUserSignal(t, firstInvariantRead, "first invariant read")

	go func() { secondResult <- run(secondMutation) }()
	waitForUserLockAttempt(t, lockAttempts, 2)
	if got := lockAcquisitions.Load(); got != 1 {
		t.Fatalf("lock acquisitions before release = %d, want 1", got)
	}
	if got := validatedMutations.Load(); got != 1 {
		t.Fatalf("validated mutations before release = %d, want 1", got)
	}

	close(releaseFirst)
	firstReleased = true
	firstRec := waitForUserMutationResult(t, firstResult, "first mutation")
	secondRec := waitForUserMutationResult(t, secondResult, "second mutation")
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", firstRec.Code, firstRec.Body.String())
	}
	if secondRec.Code != http.StatusConflict || responseCode(t, secondRec) != "LAST_ADMIN" {
		t.Fatalf("second status=%d body=%s", secondRec.Code, secondRec.Body.String())
	}
	if got := validatedMutations.Load(); got != 1 {
		t.Fatalf("successful invariant validations = %d, want 1", got)
	}
	if got := guardedEntries.Load(); got != 2 {
		t.Fatalf("guarded entries = %d, want 2", got)
	}
	if got := lockAttemptCount.Load(); got != 2 {
		t.Fatalf("lock attempts = %d, want 2", got)
	}
	if got := lockAcquisitions.Load(); got != 2 {
		t.Fatalf("lock acquisitions = %d, want 2", got)
	}
	if got := lockReleases.Load(); got != 2 {
		t.Fatalf("lock releases = %d, want 2", got)
	}
	if got := maximumGuarded.Load(); got != 1 {
		t.Fatalf("maximum concurrent guarded entries = %d, want 1", got)
	}
	if got := currentGuarded.Load(); got != 0 {
		t.Fatalf("current guarded entries = %d, want 0", got)
	}
	if got := guardedExits.Load(); got != 2 {
		t.Fatalf("guarded exits = %d, want 2", got)
	}
	var active int64
	if err := database.Model(&db.User{}).Where("role = ? AND enabled = ?", "admin", true).Count(&active).Error; err != nil || active != 1 {
		t.Fatalf("active admins=%d err=%v", active, err)
	}
}

func waitForUserSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForUserMutationResult(t *testing.T, results <-chan *httptest.ResponseRecorder, description string) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return nil
	}
}

func waitForUserLockAttempt(t *testing.T, attempts <-chan int32, want int32) {
	t.Helper()
	select {
	case got := <-attempts:
		if got != want {
			t.Fatalf("lock attempt = %d, want %d", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for lock attempt %d", want)
	}
}

func firstUser(first, _ db.User) uint   { return first.ID }
func secondUser(_, second db.User) uint { return second.ID }

func TestConcurrentAdminDemotionsLeaveOneEnabledAdmin(t *testing.T) {
	demoteSecond := concurrentUserMutation{method: http.MethodPut, body: `{"role":"readonly"}`, actor: firstUser, target: secondUser}
	demoteFirst := concurrentUserMutation{method: http.MethodPut, body: `{"role":"readonly"}`, actor: secondUser, target: firstUser}
	runDeterministicAdminRace(t, demoteSecond, demoteFirst)
}

func TestConcurrentAdminDeletesLeaveOneEnabledAdmin(t *testing.T) {
	deleteSecond := concurrentUserMutation{method: http.MethodDelete, actor: firstUser, target: secondUser}
	deleteFirst := concurrentUserMutation{method: http.MethodDelete, actor: secondUser, target: firstUser}
	runDeterministicAdminRace(t, deleteSecond, deleteFirst)
}

func TestConcurrentAdminDeleteAndDemotionLeaveOneEnabledAdmin(t *testing.T) {
	deleteSecond := concurrentUserMutation{method: http.MethodDelete, actor: firstUser, target: secondUser}
	disableFirst := concurrentUserMutation{method: http.MethodPut, body: `{"enabled":false}`, actor: secondUser, target: firstUser}
	runDeterministicAdminRace(t, deleteSecond, disableFirst)
}

func TestUserRequestValidation(t *testing.T) {
	r, database := newUserTestRouter(t)
	actor := createUserForMutationTest(t, database, "owner", "admin", true)
	target := createUserForMutationTest(t, database, "target", "readonly", true)
	path := "/users/" + strconv.FormatUint(uint64(target.ID), 10)
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "invalid id", method: http.MethodPut, path: "/users/nope", body: `{"enabled":true}`},
		{name: "zero id", method: http.MethodDelete, path: "/users/0"},
		{name: "invalid role", method: http.MethodPut, path: path, body: `{"role":"owner"}`},
		{name: "empty role", method: http.MethodPut, path: path, body: `{"role":""}`},
		{name: "short password", method: http.MethodPut, path: path, body: `{"password":"abc"}`},
		{name: "empty password", method: http.MethodPut, path: path, body: `{"password":""}`},
		{name: "empty update", method: http.MethodPut, path: path, body: `{}`},
		{name: "null update", method: http.MethodPut, path: path, body: `{"role":null}`},
		{name: "malformed update", method: http.MethodPut, path: path, body: `{`},
		{name: "create invalid role", method: http.MethodPost, path: "/users", body: `{"username":"new","password":"valid-password","role":"owner"}`},
		{name: "create short password", method: http.MethodPost, path: "/users", body: `{"username":"new","password":"abc","role":"readonly"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := userMutationRequest(r, tc.method, tc.path, tc.body, actor.ID)
			if rec.Code != http.StatusBadRequest || responseCode(t, rec) != "BAD_REQUEST" {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUserLookupsDistinguishMissingFromDatabaseFailure(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		r, database := newUserTestRouter(t)
		actor := createUserForMutationTest(t, database, "owner", "admin", true)
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			body := ""
			if method == http.MethodPut {
				body = `{"enabled":true}`
			}
			rec := userMutationRequest(r, method, "/users/99999", body, actor.ID)
			if rec.Code != http.StatusNotFound || responseCode(t, rec) != "NOT_FOUND" {
				t.Fatalf("method=%s status=%d body=%s", method, rec.Code, rec.Body.String())
			}
		}
	})

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		t.Run("failure "+method, func(t *testing.T) {
			r, database := newUserTestRouter(t)
			actor := createUserForMutationTest(t, database, "owner", "admin", true)
			target := createUserForMutationTest(t, database, "target", "readonly", true)
			injectUserGORMFailure(t, database, "query", "users", 1)
			body := ""
			if method == http.MethodPut {
				body = `{"enabled":true}`
			}
			rec := userMutationRequest(r, method, "/users/"+strconv.FormatUint(uint64(target.ID), 10), body, actor.ID)
			if rec.Code != http.StatusInternalServerError || responseCode(t, rec) != "DB_ERROR" {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUserListPropagatesDatabaseFailure(t *testing.T) {
	r, database := newUserTestRouter(t)
	actor := createUserForMutationTest(t, database, "owner", "admin", true)
	injectUserGORMFailure(t, database, "query", "users", 1)
	rec := userMutationRequest(r, http.MethodGet, "/users", "", actor.ID)
	if rec.Code != http.StatusInternalServerError || responseCode(t, rec) != "DB_ERROR" {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUserCreateDistinguishesConflictFromDatabaseFailure(t *testing.T) {
	t.Run("duplicate", func(t *testing.T) {
		r, database := newUserTestRouter(t)
		actor := createUserForMutationTest(t, database, "owner", "admin", true)
		createUserForMutationTest(t, database, "duplicate", "readonly", true)
		rec := userMutationRequest(r, http.MethodPost, "/users", `{"username":"duplicate","password":"valid-password","role":"readonly"}`, actor.ID)
		if rec.Code != http.StatusConflict || responseCode(t, rec) != "CONFLICT" {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("database failure", func(t *testing.T) {
		r, database := newUserTestRouter(t)
		actor := createUserForMutationTest(t, database, "owner", "admin", true)
		injectUserGORMFailure(t, database, "create", "users", 1)
		rec := userMutationRequest(r, http.MethodPost, "/users", `{"username":"new-user","password":"valid-password","role":"readonly"}`, actor.ID)
		if rec.Code != http.StatusInternalServerError || responseCode(t, rec) != "DB_ERROR" {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestUserUpdateFailureDoesNotReportSuccessOrPersistPartialChanges(t *testing.T) {
	r, database := newUserTestRouter(t)
	actor := createUserForMutationTest(t, database, "owner", "admin", true)
	target := createUserForMutationTest(t, database, "target", "readonly", true)
	injectUserGORMFailure(t, database, "update", "users", 1)
	rec := userMutationRequest(r, http.MethodPut, "/users/"+strconv.FormatUint(uint64(target.ID), 10), `{"password":"new-password","role":"admin"}`, actor.ID)
	if rec.Code != http.StatusInternalServerError || responseCode(t, rec) != "DB_ERROR" {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var saved db.User
	if err := database.First(&saved, target.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if saved.Role != "readonly" || bcrypt.CompareHashAndPassword([]byte(saved.PasswordHash), []byte("old-password")) != nil {
		t.Fatalf("partial update persisted: %#v", saved)
	}
}

func TestUserUpdateRollsBackWhenReloadFails(t *testing.T) {
	r, database := newUserTestRouter(t)
	actor := createUserForMutationTest(t, database, "owner", "admin", true)
	target := createUserForMutationTest(t, database, "target", "readonly", true)
	injectUserGORMFailure(t, database, "query", "users", 2)
	rec := userMutationRequest(r, http.MethodPut, "/users/"+strconv.FormatUint(uint64(target.ID), 10), `{"role":"admin"}`, actor.ID)
	if rec.Code != http.StatusInternalServerError || responseCode(t, rec) != "DB_ERROR" {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var saved db.User
	if err := database.First(&saved, target.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if saved.Role != "readonly" {
		t.Fatalf("role persisted despite rollback: %q", saved.Role)
	}
}

func TestUserMutationsStopWhenAdminCountFails(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			r, database := newUserTestRouter(t)
			actor := createUserForMutationTest(t, database, "actor", "readonly", true)
			target := createUserForMutationTest(t, database, "admin", "admin", true)
			injectUserGORMFailure(t, database, "query", "users", 2)
			body := ""
			if method == http.MethodPut {
				body = `{"enabled":false}`
			}
			rec := userMutationRequest(r, method, "/users/"+strconv.FormatUint(uint64(target.ID), 10), body, actor.ID)
			if rec.Code != http.StatusInternalServerError || responseCode(t, rec) != "DB_ERROR" {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var saved db.User
			if err := database.First(&saved, target.ID).Error; err != nil {
				t.Fatalf("reload: %v", err)
			}
			if !effectiveAdmin(saved) {
				t.Fatalf("admin changed despite count failure: %#v", saved)
			}
		})
	}
}

func TestUserDeleteRollsBackUserAndTokensOnFailure(t *testing.T) {
	for _, failTable := range []string{"api_tokens", "users"} {
		t.Run(failTable, func(t *testing.T) {
			r, database := newUserTestRouter(t)
			actor := createUserForMutationTest(t, database, "owner", "admin", true)
			target := createUserForMutationTest(t, database, "target", "readonly", true)
			token := db.APIToken{UserID: target.ID, Name: "target-token", TokenHash: "hash-" + failTable, Permissions: "read"}
			if err := database.Create(&token).Error; err != nil {
				t.Fatalf("create token: %v", err)
			}
			injectUserGORMFailure(t, database, "delete", failTable, 1)
			rec := userMutationRequest(r, http.MethodDelete, "/users/"+strconv.FormatUint(uint64(target.ID), 10), "", actor.ID)
			if rec.Code != http.StatusInternalServerError || responseCode(t, rec) != "DB_ERROR" {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var userCount, tokenCount int64
			if err := database.Model(&db.User{}).Where("id = ?", target.ID).Count(&userCount).Error; err != nil {
				t.Fatalf("count user: %v", err)
			}
			if err := database.Model(&db.APIToken{}).Where("id = ?", token.ID).Count(&tokenCount).Error; err != nil {
				t.Fatalf("count token: %v", err)
			}
			if userCount != 1 || tokenCount != 1 {
				t.Fatalf("rollback failed: user=%d token=%d", userCount, tokenCount)
			}
		})
	}
}

func TestUserDeleteRemovesAPITokensTransactionally(t *testing.T) {
	r, database := newUserTestRouter(t)
	actor := createUserForMutationTest(t, database, "owner", "admin", true)
	target := createUserForMutationTest(t, database, "target", "readonly", true)
	token := db.APIToken{UserID: target.ID, Name: "target-token", TokenHash: "target-hash", Permissions: "read"}
	if err := database.Create(&token).Error; err != nil {
		t.Fatalf("create token: %v", err)
	}
	rec := userMutationRequest(r, http.MethodDelete, "/users/"+strconv.FormatUint(uint64(target.ID), 10), "", actor.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var tokenCount int64
	if err := database.Model(&db.APIToken{}).Where("user_id = ?", target.ID).Count(&tokenCount).Error; err != nil || tokenCount != 0 {
		t.Fatalf("token count=%d err=%v", tokenCount, err)
	}
}
