// User Register Endpoint Load Test
// Tests POST /user/register endpoint
//
// Usage:
//   K6_VUS=100 K6_DURATION=5m k6 run scenarios/user-register.js
//   K6_START_VUS=10 K6_END_VUS=200 K6_STEP_VUS=20 k6 run scenarios/user-register.js

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';
import { htmlReport } from '../lib/vendor/k6-reporter.js';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.2/index.js';
import { buildCapacityStages, getCapacityConfig, getFixedConfig, baseUrl, printConfig } from '../lib/config.js';
import {
  buildRampingStepPlan,
  buildFixedStepPlan,
  createStepTracker,
  recordHttpSteadyMetrics,
  buildHttpStepReport,
  buildHttpStepThresholds,
} from '../lib/step-report.js';

// Custom metrics
var registerSuccessRate = new Rate('register_success_rate');
var registerDuration = new Trend('register_duration');
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
    register_test: scenario,
  },
  thresholds: Object.assign(
    {
      http_req_duration: ['p(95)<500'],
      http_req_failed: ['rate<0.05'],
      register_success_rate: ['rate>0.90'],
    },
    stepThresholds
  ),
  summaryTrendStats: ['avg', 'p(90)', 'p(95)', 'p(99)', 'min', 'max'],
};

// Setup
export function setup() {
  printConfig();
  return {};
}

// Main test function
export default function () {
  var stepInfo = stepTracker.getStepInfo();
  var requestTags = stepTracker.getRequestTags(stepInfo);
  var steadyTags = stepTracker.getSteadyTags(stepInfo);

  if (steadyTags) {
    steadyIters.add(1, steadyTags);
  }

  // Each registration creates a unique user
  var timestamp = Date.now();
  var payload = JSON.stringify({
    userName: 'register_' + __VU + '_' + __ITER + '_' + timestamp,
    passWord: 'loadtest123',
  });

  var params = {
    headers: { 'Content-Type': 'application/json' },
    tags: Object.assign({ name: 'register' }, requestTags),
  };

  var startTime = Date.now();
  var res = http.post(baseUrl + '/user/register', payload, params);
  var duration = Math.max(0, Date.now() - startTime);

  registerDuration.add(duration);
  recordHttpSteadyMetrics(res, duration, steadyTags, steadyMetrics);

  var success = check(res, {
    'status is 200': function (r) { return r.status === 200; },
    'register successful': function (r) {
      try {
        var body = JSON.parse(r.body);
        return body.code === 0 && body.data;
      } catch (e) {
        return false;
      }
    },
  });

  registerSuccessRate.add(success);

  sleep(0.5 + Math.random() * 0.5);
}

// Generate reports
export function handleSummary(data) {
  var stepReport = buildHttpStepReport(data, stepPlan, { title: 'User Register Step Report' });

  return {
    '/reports/user-register.html': stepReport.html,
    '/reports/user-register-steps.json': JSON.stringify(stepReport.json, null, 2),
    '/reports/user-register-full.html': htmlReport(data),
    '/reports/user-register.json': JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
  };
}
