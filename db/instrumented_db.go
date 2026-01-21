package db

import (
	"time"

	"gochat/pkg/metrics"

	"github.com/jinzhu/gorm"
)

const (
	callbackPrefix = "gochat_metrics"
	startTimeKey   = "gochat_start_time"
)

// RegisterMetricsCallbacks registers GORM callbacks to track query metrics.
// Call this after opening the database connection.
func RegisterMetricsCallbacks(db *gorm.DB, serviceName string) {
	// Create callbacks
	db.Callback().Create().Before("gorm:create").Register(callbackPrefix+"_before_create", beforeCallback)
	db.Callback().Create().After("gorm:create").Register(callbackPrefix+"_after_create", afterCallback(serviceName, "insert"))

	db.Callback().Query().Before("gorm:query").Register(callbackPrefix+"_before_query", beforeCallback)
	db.Callback().Query().After("gorm:query").Register(callbackPrefix+"_after_query", afterCallback(serviceName, "select"))

	db.Callback().Update().Before("gorm:update").Register(callbackPrefix+"_before_update", beforeCallback)
	db.Callback().Update().After("gorm:update").Register(callbackPrefix+"_after_update", afterCallback(serviceName, "update"))

	db.Callback().Delete().Before("gorm:delete").Register(callbackPrefix+"_before_delete", beforeCallback)
	db.Callback().Delete().After("gorm:delete").Register(callbackPrefix+"_after_delete", afterCallback(serviceName, "delete"))

	db.Callback().RowQuery().Before("gorm:row_query").Register(callbackPrefix+"_before_row_query", beforeCallback)
	db.Callback().RowQuery().After("gorm:row_query").Register(callbackPrefix+"_after_row_query", afterCallback(serviceName, "raw"))
}

func beforeCallback(scope *gorm.Scope) {
	scope.Set(startTimeKey, time.Now())
}

func afterCallback(serviceName, operation string) func(*gorm.Scope) {
	return func(scope *gorm.Scope) {
		startTime, ok := scope.Get(startTimeKey)
		if !ok {
			return
		}
		start, ok := startTime.(time.Time)
		if !ok {
			return
		}

		duration := time.Since(start).Seconds()
		table := scope.TableName()
		if table == "" {
			table = "unknown"
		}

		status := "success"
		if scope.HasError() {
			status = "error"
		}

		metrics.DBQueryDuration.WithLabelValues(serviceName, operation, table).Observe(duration)
		metrics.DBQueryTotal.WithLabelValues(serviceName, operation, table, status).Inc()
	}
}

// InitInstrumentedDB initializes the database with metrics callbacks.
// This is a convenience function that wraps GetDb and registers callbacks.
func InitInstrumentedDB(dbName, serviceName string) *gorm.DB {
	db := GetDb(dbName)
	if db != nil {
		RegisterMetricsCallbacks(db, serviceName)
	}
	return db
}
