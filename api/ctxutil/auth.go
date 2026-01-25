// Package ctxutil provides utilities for working with gin context
package ctxutil

import "github.com/gin-gonic/gin"

// Context keys for storing auth info
const (
	CtxKeyUserId   = "auth_user_id"
	CtxKeyUserName = "auth_user_name"
)

// SetAuthToContext stores auth info in gin context
func SetAuthToContext(c *gin.Context, userId int, userName string) {
	c.Set(CtxKeyUserId, userId)
	c.Set(CtxKeyUserName, userName)
}

// GetAuthFromContext retrieves cached auth info from context
func GetAuthFromContext(c *gin.Context) (userId int, userName string, ok bool) {
	userIdVal, exists := c.Get(CtxKeyUserId)
	if !exists {
		return 0, "", false
	}
	userNameVal, exists := c.Get(CtxKeyUserName)
	if !exists {
		return 0, "", false
	}
	userId, ok = userIdVal.(int)
	if !ok {
		return 0, "", false
	}
	userName, ok = userNameVal.(string)
	return userId, userName, ok
}
