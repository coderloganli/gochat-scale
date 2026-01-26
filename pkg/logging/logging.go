package logging

import (
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

// InitFromEnv sets logrus level from LOG_LEVEL when provided.
func InitFromEnv() {
	levelStr := strings.TrimSpace(strings.ToLower(os.Getenv("LOG_LEVEL")))
	if levelStr == "" {
		return
	}
	switch levelStr {
	case "trace":
		logrus.SetLevel(logrus.TraceLevel)
	case "debug":
		logrus.SetLevel(logrus.DebugLevel)
	case "info":
		logrus.SetLevel(logrus.InfoLevel)
	case "warn", "warning":
		logrus.SetLevel(logrus.WarnLevel)
	case "error":
		logrus.SetLevel(logrus.ErrorLevel)
	case "fatal":
		logrus.SetLevel(logrus.FatalLevel)
	case "panic":
		logrus.SetLevel(logrus.PanicLevel)
	}
}
