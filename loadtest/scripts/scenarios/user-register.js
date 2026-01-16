// User Register Endpoint Load Test
// Tests POST /user/register endpoint
//
// Usage:
//   K6_VUS=100 K6_DURATION=5m k6 run scenarios/user-register.js
//   K6_START_VUS=10 K6_END_VUS=200 K6_STEP_VUS=20 k6 run scenarios/user-register.js

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';
import { htmlReport } from '../lib/vendor/k6-reporter.js';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.2/index.js';
import { buildStages, getFixedConfig, baseUrl, printConfig } from '../lib/config.js';

// Custom metrics
var registerSuccessRate = new Rate('register_success_rate');
var registerDuration = new Trend('register_duration');

// Build options
var useFixedVus = __ENV.K6_VUS !== undefined && __ENV.K6_VUS !== '';
var fixedConfig = getFixedConfig();
var stages = buildStages();

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
  thresholds: {
    http_req_duration: ['p(95)<500'],
    http_req_failed: ['rate<0.05'],
    register_success_rate: ['rate>0.90'],
  },
};

// Setup
export function setup() {
  printConfig();
  return {};
}

// Main test function
export default function () {
  // Each registration creates a unique user
  var timestamp = Date.now();
  var payload = JSON.stringify({
    userName: 'register_' + __VU + '_' + __ITER + '_' + timestamp,
    passWord: 'loadtest123',
  });

  var params = {
    headers: { 'Content-Type': 'application/json' },
    tags: { name: 'register' },
  };

  var startTime = Date.now();
  var res = http.post(baseUrl + '/user/register', payload, params);
  var duration = Date.now() - startTime;

  registerDuration.add(duration);

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
  return {
    '/reports/user-register.html': htmlReport(data),
    '/reports/user-register.json': JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
  };
}
