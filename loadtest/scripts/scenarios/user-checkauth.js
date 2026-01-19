// User CheckAuth Endpoint Load Test
// Tests POST /user/checkAuth endpoint
//
// Usage:
//   K6_VUS=100 K6_DURATION=5m k6 run scenarios/user-checkauth.js
//   K6_START_VUS=10 K6_END_VUS=200 K6_STEP_VUS=20 k6 run scenarios/user-checkauth.js

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';
import { htmlReport } from '../lib/vendor/k6-reporter.js';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.2/index.js';
import { buildCapacityStages, getCapacityConfig, getFixedConfig, baseUrl, printConfig } from '../lib/config.js';
import { createTestUsers } from '../lib/auth.js';
import {
  buildRampingStepPlan,
  buildFixedStepPlan,
  createStepTracker,
  recordHttpSteadyMetrics,
  buildHttpStepReport,
  buildHttpStepThresholds,
} from '../lib/step-report.js';

// Custom metrics
var checkAuthSuccessRate = new Rate('checkauth_success_rate');
var checkAuthDuration = new Trend('checkauth_duration');
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
    checkauth_test: scenario,
  },
  thresholds: Object.assign(
    {
      http_req_duration: ['p(95)<300'],
      http_req_failed: ['rate<0.05'],
      checkauth_success_rate: ['rate>0.95'],
    },
    stepThresholds
  ),
  summaryTrendStats: ['avg', 'p(90)', 'p(95)', 'p(99)', 'min', 'max'],
};

// Setup: Create test users
export function setup() {
  printConfig();

  var userCount = useFixedVus ? fixedConfig.vus : parseInt(__ENV.K6_END_VUS) || 100;
  var users = createTestUsers(Math.min(userCount, 100), 'checkauth_test');

  return { users: users };
}

// Main test function
export default function (data) {
  var stepInfo = stepTracker.getStepInfo();
  var requestTags = stepTracker.getRequestTags(stepInfo);
  var steadyTags = stepTracker.getSteadyTags(stepInfo);

  if (steadyTags) {
    steadyIters.add(1, steadyTags);
  }

  var userIndex = __VU % Math.max(data.users.length, 1);
  var user = data.users.length > 0 ? data.users[userIndex] : null;

  if (!user || !user.authToken) {
    checkAuthSuccessRate.add(false);
    sleep(0.5);
    return;
  }

  var payload = JSON.stringify({
    authToken: user.authToken,
  });

  var params = {
    headers: { 'Content-Type': 'application/json' },
    tags: Object.assign({ name: 'checkAuth' }, requestTags),
  };

  var startTime = Date.now();
  var res = http.post(baseUrl + '/user/checkAuth', payload, params);
  var duration = Math.max(0, Date.now() - startTime);

  checkAuthDuration.add(duration);
  recordHttpSteadyMetrics(res, duration, steadyTags, steadyMetrics);

  var success = check(res, {
    'status is 200': function (r) { return r.status === 200; },
    'checkAuth successful': function (r) {
      try {
        var body = JSON.parse(r.body);
        return body.code === 0;
      } catch (e) {
        return false;
      }
    },
    'returns userId': function (r) {
      try {
        var body = JSON.parse(r.body);
        return body.data && body.data.userId;
      } catch (e) {
        return false;
      }
    },
  });

  checkAuthSuccessRate.add(success);

  sleep(0.3 + Math.random() * 0.4);
}

// Generate reports
export function handleSummary(data) {
  var stepReport = buildHttpStepReport(data, stepPlan, { title: 'User CheckAuth Step Report' });

  return {
    '/reports/user-checkauth.html': stepReport.html,
    '/reports/user-checkauth-steps.json': JSON.stringify(stepReport.json, null, 2),
    '/reports/user-checkauth-full.html': htmlReport(data),
    '/reports/user-checkauth.json': JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
  };
}
