/**
 * Created by lock
 * Date: 2019-10-06
 * Time: 23:30
 */
package tools

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

const (
	CodeSuccess      = 0
	CodeFail         = 1
	CodeUnknownError = -1
	CodeSessionError = 40000
	// CodeOverloaded is returned when the request was refused without being
	// processed, because the service was already at its in-flight limit.
	CodeOverloaded = 42900
	// CodeUnavailable is returned when an upstream dependency timed out or
	// could not be reached.
	CodeUnavailable = 50300
)

var MsgCodeMap = map[int]string{
	CodeUnknownError: "unKnow error",
	CodeSuccess:      "success",
	CodeFail:         "fail",
	CodeSessionError: "Session error",
	CodeOverloaded:   "server is busy, retry shortly",
	CodeUnavailable:  "upstream unavailable",
}

// httpStatusByCode maps a business code to the HTTP status it is returned with.
//
// Historically every response was HTTP 200 with the real outcome buried in the
// body, which means no HTTP client — a browser, a proxy, a load test — can tell
// a refusal from a success without parsing it. Codes that describe the state of
// the *service* rather than the outcome of a request now carry a matching
// status, so they are visible to anything counting statuses.
//
// Codes not listed here stay on 200 deliberately. A wrong password is a normal
// outcome of a working login endpoint, and returning 401 for it would trip the
// frontend's session-expiry handler and reload the page out from under the user.
var httpStatusByCode = map[int]int{
	CodeSessionError: http.StatusUnauthorized,
	CodeOverloaded:   http.StatusTooManyRequests,
	CodeUnavailable:  http.StatusServiceUnavailable,
}

// HTTPStatusFor returns the HTTP status a business code is sent with.
func HTTPStatusFor(msgCode int) int {
	if status, ok := httpStatusByCode[msgCode]; ok {
		return status
	}
	return http.StatusOK
}

func SuccessWithMsg(c *gin.Context, msg interface{}, data interface{}) {
	ResponseWithCode(c, CodeSuccess, msg, data)
}

func FailWithMsg(c *gin.Context, msg interface{}) {
	ResponseWithCode(c, CodeFail, msg, nil)
}

// FailFromCode responds to a failed call, preserving the distinction between a
// request the service rejected and a dependency that let it down. The first is a
// normal outcome; the second means the service itself is unhealthy, and only the
// second should count against an availability target.
func FailFromCode(c *gin.Context, msgCode int, msg interface{}) {
	if msgCode == CodeUnavailable {
		ResponseWithCode(c, CodeUnavailable, msg, nil)
		return
	}
	FailWithMsg(c, msg)
}

// FailWithUnavailable reports that an upstream dependency failed, rather than
// that the request was invalid. Returned as HTTP 503.
func FailWithUnavailable(c *gin.Context, msg interface{}) {
	ResponseWithCode(c, CodeUnavailable, msg, nil)
}

func ResponseWithCode(c *gin.Context, msgCode int, msg interface{}, data interface{}) {
	if msg == nil {
		if val, ok := MsgCodeMap[msgCode]; ok {
			msg = val
		} else {
			msg = MsgCodeMap[-1]
		}
	}

	c.AbortWithStatusJSON(HTTPStatusFor(msgCode), gin.H{
		"code":    msgCode,
		"message": msg,
		"data":    data,
	})
}
