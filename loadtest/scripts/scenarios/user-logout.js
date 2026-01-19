// User Logout Endpoint Load Test
// Tests POST /user/logout endpoint
//
// Usage:
//   K6_VUS=100 K6_DURATION=5m k6 run scenarios/user-logout.js
//   K6_START_VUS=10 K6_END_VUS=200 K6_STEP_VUS=20 k6 run scenarios/user-logout.js

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';
import { htmlReport } from '../lib/vendor/k6-reporter.js';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.2/index.js';
import { buildCapacityStages, getCapacityConfig, getFixedConfig, baseUrl, printConfig } from '../lib/config.js';
import { registerUser, loginUser } from '../lib/auth.js';
import {
  buildRampingStepPlan,
  buildFixedStepPlan,
  createStepTracker,
  recordHttpSteadyMetrics,
  buildHttpStepReport,
  buildHttpStepThresholds,
} from '../lib/step-report.js';

// Custom metrics
var logoutSuccessRate = new Rate('logout_success_rate');
var logoutDuration = new Trend('logout_duration');
var steadyHttpDuration = new Trend('steady_http_duration');
var steadyHttpRequests = new Counter('steady_http_requests');
var steadyHttpErrors = new Counter('steady_http_errors');
var steadyHttp4xx = new Counter('steady_http_4xx');
var steadyHttp5xx = new Counter('steady_http_5xx');
var steadyHttp429 = new Counter('steady_http_429');
var steadyHttpTimeout = new Counter('steady_http_timeout');
var steadyIters = new Counter('steady_iters');

var steadyMetrics = {
  duration: steadyHttpDuration,
  requests: steadyHttpRequests,
  errors: steadyHttpErrors,
  http4xx: steadyHttp4xx,
  http5xx: steadyHttp5xx,
  http429: steadyHttp429,
  timeouts: steadyHttpTimeout,
};

// Build options
var useFixedVus = __ENV.K6_VUS !== undefined && __ENV.K6_VUS !== '';
var fixedConfig = getFixedConfig();
var capacityConfig = getCapacityConfig();
var stages = buildCapacityStages();
var stepPlan = useFixedVus
  ? buildFixedStepPlan(fixedConfig.duration, capacityConfig.warmupDuration, fixedConfig.vus)
  : buildRampingStepPlan(capacityConfig);
var stepTracker = createStepTracker(stepPlan);
var stepThresholds = buildHttpStepThresholds(stepPlan);

var scenario = {};
if (useFixedVus) {
  scenario = {
    executor: 'constant-vus',
    vus: fixedConfig.vus,
    duration: fixedConfig.duration,
  };
} else {
  scenario = {
    executor: 'ramping-vus',
    startVUs: 0,
    stages: stages,
    gracefulRampDown: '30s',
  };
}

export var options = {
  scenarios: {
    logout_test: scenario,
  },
  thresholds: Object.assign(
    {
      http_req_duration: ['p(95)<500'],
      http_req_failed: ['rate<0.05'],
      logout_success_rate: ['rate>0.95'],
    },
    stepThresholds
  ),
  summaryTrendStats: ['avg', 'p(90)', 'p(95)', 'p(99)', 'min', 'max'],
};

// Setup: Create test users
export function setup() {
  printConfig();
  return {};
}

// Main test function
// Flow: Register -> Login -> Logout (tests full lifecycle)
export default function () {
  var stepInfo = stepTracker.getStepInfo();
  var requestTags = stepTracker.getRequestTags(stepInfo);
  var steadyTags = stepTracker.getSteadyTags(stepInfo);

  if (steadyTags) {
    steadyIters.add(1, steadyTags);
  }

  // Create unique user for this iteration
  var timestamp = Date.now();
  var userName = 'logout_test_' + __VU + '_' + __ITER + '_' + timestamp;
  var password = 'loadtest123';

  // Step 1: Register user
  var authToken = registerUser(userName, password);
  if (!authToken) {
    // Try login if register fails (user might exist)
    authToken = loginUser(userName, password);
  }

  if (!authToken) {
    logoutSuccessRate.add(false);
    sleep(0.5);
    return;
  }

  // Step 2: Logout
  var payload = JSON.stringify({
    authToken: authToken,
  });

  var params = {
    headers: { 'Content-Type': 'application/json' },
    tags: Object.assign({ name: 'logout' }, requestTags),
  };

  var startTime = Date.now();
  var res = http.post(baseUrl + '/user/logout', payload, params);
  var duration = Math.max(0, Date.now() - startTime);

  logoutDuration.add(duration);
  recordHttpSteadyMetrics(res, duration, steadyTags, steadyMetrics);

  var success = check(res, {
    'status is 200': function (r) { return r.status === 200; },
    'logout successful': function (r) {
      try {
        var body = JSON.parse(r.body);
        return body.code === 0;
      } catch (e) {
        return false;
      }
    },
  });

  logoutSuccessRate.add(success);

  sleep(0.5 + Math.random() * 0.5);
}

// Generate reports
export function handleSummary(data) {
  var stepReport = buildHttpStepReport(data, stepPlan, { title: 'User Logout Step Report' });

  return {
    '/reports/user-logout.html': stepReport.html,
    '/reports/user-logout-steps.json': JSON.stringify(stepReport.json, null, 2),
    '/reports/user-logout-full.html': htmlReport(data),
    '/reports/user-logout.json': JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
  };
}
