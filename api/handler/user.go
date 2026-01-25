/**
 * Created by lock
 * Date: 2019-10-06
 * Time: 23:40
 */
package handler

import (
	"gochat/api/cache"
	"gochat/api/ctxutil"
	"gochat/api/rpc"
	"gochat/pkg/metrics"
	"gochat/proto"
	"gochat/tools"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

type FormLogin struct {
	UserName string `form:"userName" json:"userName" binding:"required"`
	Password string `form:"passWord" json:"passWord" binding:"required"`
}

func Login(c *gin.Context) {
	var formLogin FormLogin
	if err := c.ShouldBindBodyWith(&formLogin, binding.JSON); err != nil {
		tools.FailWithMsg(c, err.Error())
		return
	}
	req := &proto.LoginRequest{
		Name:     formLogin.UserName,
		Password: tools.Sha1(formLogin.Password),
	}
	code, authToken, msg := rpc.RpcLogicObj.Login(c.Request.Context(), req)
	status := "success"
	if code == tools.CodeFail || authToken == "" {
		status = "failure"
		metrics.UserOperationsTotal.WithLabelValues("login", status).Inc()
		tools.FailWithMsg(c, msg)
		return
	}
	metrics.UserOperationsTotal.WithLabelValues("login", status).Inc()
	tools.SuccessWithMsg(c, "login success", authToken)
}

type FormRegister struct {
	UserName string `form:"userName" json:"userName" binding:"required"`
	Password string `form:"passWord" json:"passWord" binding:"required"`
}

func Register(c *gin.Context) {
	var formRegister FormRegister
	if err := c.ShouldBindBodyWith(&formRegister, binding.JSON); err != nil {
		tools.FailWithMsg(c, err.Error())
		return
	}
	req := &proto.RegisterRequest{
		Name:     formRegister.UserName,
		Password: tools.Sha1(formRegister.Password),
	}
	code, authToken, msg := rpc.RpcLogicObj.Register(c.Request.Context(), req)
	status := "success"
	if code == tools.CodeFail || authToken == "" {
		status = "failure"
		metrics.UserOperationsTotal.WithLabelValues("register", status).Inc()
		tools.FailWithMsg(c, msg)
		return
	}
	metrics.UserOperationsTotal.WithLabelValues("register", status).Inc()
	tools.SuccessWithMsg(c, "register success", authToken)
}

type FormCheckAuth struct {
	AuthToken string `form:"authToken" json:"authToken" binding:"required"`
}

func CheckAuth(c *gin.Context) {
	// Auth already validated by middleware, just return cached info from context
	userId, userName, ok := ctxutil.GetAuthFromContext(c)
	if !ok {
		tools.FailWithMsg(c, "auth fail")
		return
	}
	var jsonData = map[string]interface{}{
		"userId":   userId,
		"userName": userName,
	}
	tools.SuccessWithMsg(c, "auth success", jsonData)
}

type FormLogout struct {
	AuthToken string `form:"authToken" json:"authToken" binding:"required"`
}

func Logout(c *gin.Context) {
	var formLogout FormLogout
	if err := c.ShouldBindBodyWith(&formLogout, binding.JSON); err != nil {
		tools.FailWithMsg(c, err.Error())
		return
	}
	authToken := formLogout.AuthToken

	// Clear from local cache first
	cache.GetAuthCache().Delete(authToken)

	logoutReq := &proto.LogoutRequest{
		AuthToken: authToken,
	}
	code := rpc.RpcLogicObj.Logout(c.Request.Context(), logoutReq)
	if code == tools.CodeFail {
		tools.FailWithMsg(c, "logout fail!")
		return
	}
	tools.SuccessWithMsg(c, "logout ok!", nil)
}
