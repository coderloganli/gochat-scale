/**
 * Created by lock
 * Date: 2019-10-06
 * Time: 23:09
 */
package router

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"gochat/api/cache"
	"gochat/api/ctxutil"
	"gochat/api/handler"
	"gochat/api/rpc"
	"gochat/config"
	"gochat/pkg/middleware"
	"gochat/proto"
	"gochat/tools"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/sirupsen/logrus"
)

func Register() *gin.Engine {
	r := gin.Default()
	r.Use(CorsMiddleware())
	r.Use(middleware.TracingMiddleware("api"))
	r.Use(middleware.PrometheusMiddleware("api"))
	// After the metrics middleware so that shed requests are still counted, and
	// before everything else so that a shed costs nothing but the refusal.
	if admission, ok := admissionMiddleware(); ok {
		r.Use(admission)
	}
	initUserRouter(r)
	initPushRouter(r)
	r.NoRoute(func(c *gin.Context) {
		tools.FailWithMsg(c, "please check request url !")
	})
	return r
}

// admissionMiddleware builds the load shedder from config, or reports that it is
// switched off. It is disabled by config rather than removed so that a run with
// and without it can be compared on the same build.
func admissionMiddleware() (gin.HandlerFunc, bool) {
	cfg := config.Conf.Api.ApiAdmission
	// Environment overrides exist so that one deployed build can be measured with
	// admission control on and off, without a rebuild between the two runs.
	if v := os.Getenv("ADMISSION_ENABLED"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			cfg.Enabled = parsed
		} else {
			logrus.Warnf("invalid ADMISSION_ENABLED %q, using config", v)
		}
	}
	if v := os.Getenv("ADMISSION_MAX_IN_FLIGHT"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			cfg.MaxInFlight = parsed
		} else {
			logrus.Warnf("invalid ADMISSION_MAX_IN_FLIGHT %q, using config", v)
		}
	}
	if !cfg.Enabled {
		logrus.Warn("admission control disabled: the service will queue without bound under overload")
		return nil, false
	}
	opts := middleware.AdmissionOptions{MaxInFlight: cfg.MaxInFlight}
	if cfg.AcquireTimeout != "" {
		parsed, err := time.ParseDuration(cfg.AcquireTimeout)
		if err != nil || parsed <= 0 {
			logrus.Warnf("invalid acquireTimeout %q, using default", cfg.AcquireTimeout)
		} else {
			opts.AcquireTimeout = parsed
		}
	}
	return middleware.Admission("api", opts), true
}

func initUserRouter(r *gin.Engine) {
	userGroup := r.Group("/user")
	userGroup.POST("/login", handler.Login)
	userGroup.POST("/register", handler.Register)
	userGroup.Use(CheckSessionId())
	{
		userGroup.POST("/checkAuth", handler.CheckAuth)
		userGroup.POST("/logout", handler.Logout)
	}

}

func initPushRouter(r *gin.Engine) {
	pushGroup := r.Group("/push")
	pushGroup.Use(CheckSessionId())
	{
		pushGroup.POST("/push", handler.Push)
		pushGroup.POST("/pushRoom", handler.PushRoom)
		pushGroup.POST("/count", handler.Count)
		pushGroup.POST("/getRoomInfo", handler.GetRoomInfo)
	}

}

type FormCheckSessionId struct {
	AuthToken string `form:"authToken" json:"authToken" binding:"required"`
}

func CheckSessionId() gin.HandlerFunc {
	authCache := cache.GetAuthCache()

	return func(c *gin.Context) {
		var formCheckSessionId FormCheckSessionId
		if err := c.ShouldBindBodyWith(&formCheckSessionId, binding.JSON); err != nil {
			c.Abort()
			tools.ResponseWithCode(c, tools.CodeSessionError, nil, nil)
			return
		}
		authToken := formCheckSessionId.AuthToken

		// Try to get from local cache first (avoid RPC call)
		if userId, userName, found := authCache.Get(authToken); found {
			ctxutil.SetAuthToContext(c, userId, userName)
			c.Next()
			return
		}

		// Cache miss - call RPC
		req := &proto.CheckAuthRequest{
			AuthToken: authToken,
		}
		code, userId, userName := rpc.RpcLogicObj.CheckAuth(c.Request.Context(), req)
		if code == tools.CodeUnavailable {
			// The session may well be valid; logic could not be reached to say so.
			// Reporting that as a session error would log the user out on a blip.
			c.Abort()
			tools.ResponseWithCode(c, tools.CodeUnavailable, nil, nil)
			return
		}
		if code != tools.CodeSuccess || userId <= 0 || userName == "" {
			c.Abort()
			tools.ResponseWithCode(c, tools.CodeSessionError, nil, nil)
			return
		}

		// Store in local cache for future requests
		authCache.Set(authToken, userId, userName)

		// Store auth result in context for handlers to reuse
		ctxutil.SetAuthToContext(c, userId, userName)
		c.Next()
		return
	}
}

func CorsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		var openCorsFlag = true
		if openCorsFlag {
			c.Header("Access-Control-Allow-Origin", "*")
			c.Header("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept")
			c.Header("Access-Control-Allow-Methods", "GET, OPTIONS, POST, PUT, DELETE")
			c.Set("content-type", "application/json")
		}
		if method == "OPTIONS" {
			c.JSON(http.StatusOK, nil)
		}
		c.Next()
	}
}
