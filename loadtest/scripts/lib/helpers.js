// Common Helper Functions for Load Tests
import { Rate, Trend, Counter } from 'k6/metrics';

// Custom metrics factory
export function createMetrics(prefix) {
  return {
    successRate: new Rate(`${prefix}_success_rate`),
    duration: new Trend(`${prefix}_duration`),
    requests: new Counter(`${prefix}_requests`),
  };
}

// Generate random string
export function randomString(length) {
  const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
  let result = '';
  for (let i = 0; i < length; i++) {
    result += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return result;
}

// Generate random integer between min and max (inclusive)
export function randomInt(min, max) {
  return Math.floor(Math.random() * (max - min + 1)) + min;
}

// Sleep for random duration between min and max seconds
export function randomSleep(minSeconds, maxSeconds) {
  const duration = minSeconds + Math.random() * (maxSeconds - minSeconds);
  return duration;
}

// Parse JSON response safely
export function parseJsonResponse(response) {
  try {
    return JSON.parse(response.body);
  } catch (e) {
    return null;
  }
}

// Check if response is successful (status 200 and code 0)
export function isSuccessResponse(response) {
  if (response.status !== 200) {
    return false;
  }

  const body = parseJsonResponse(response);
  return body && body.code === 0;
}

// Format duration for logging
export function formatDuration(ms) {
  if (ms < 1000) {
    return `${ms}ms`;
  } else if (ms < 60000) {
    return `${(ms / 1000).toFixed(2)}s`;
  } else {
    return `${(ms / 60000).toFixed(2)}m`;
  }
}

// Get current timestamp in ISO format
export function timestamp() {
  return new Date().toISOString();
}

// Log with timestamp
export function log(message) {
  console.log(`[${timestamp()}] ${message}`);
}

// Calculate percentile from array of values
export function percentile(arr, p) {
  if (arr.length === 0) return 0;
  const sorted = arr.slice().sort((a, b) => a - b);
  const index = Math.ceil((p / 100) * sorted.length) - 1;
  return sorted[Math.max(0, index)];
}

// Summary statistics for an array
export function summarize(arr) {
  if (arr.length === 0) {
    return { min: 0, max: 0, avg: 0, p50: 0, p95: 0, p99: 0 };
  }

  const sum = arr.reduce((a, b) => a + b, 0);
  return {
    min: Math.min(...arr),
    max: Math.max(...arr),
    avg: sum / arr.length,
    p50: percentile(arr, 50),
    p95: percentile(arr, 95),
    p99: percentile(arr, 99),
  };
}
