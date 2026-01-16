// User Login Endpoint Load Test
// Tests POST /user/login endpoint
//
// Usage:
//   K6_VUS=100 K6_DURATION=5m k6 run scenarios/user-login.js
//   K6_START_VUS=10 K6_END_VUS=200 K6_STEP_VUS=20 k6 run scenarios/user-login.js

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';
import { htmlReport } from '../lib/vendor/k6-reporter.js';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.2/index.js';
import { buildStages, getFixedConfig, baseUrl, printConfig } from '../lib/config.js';
import { createTestUsers } from '../lib/auth.js';

// Custom metrics
var loginSuccessRate = new Rate('login_success_rate');
var loginDuration = new Trend('login_duration');

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
    login_test: scenario,
  },
  thresholds: {
    http_req_duration: ['p(95)<500'],
    http_req_failed: ['rate<0.05'],
    login_success_rate: ['rate>0.95'],
  },
};

// Setup: Create test users
export function setup() {
  printConfig();

  var userCount = useFixedVus ? fixedConfig.vus : parseInt(__ENV.K6_END_VUS) || 100;
  var users = createTestUsers(Math.min(userCount, 100), 'login_test');

  return { users: users };
}

// Main test function
export default function (data) {
  var userIndex = __VU % Math.max(data.users.length, 1);
  var user = data.users.length > 0 ? data.users[userIndex] : null;

  var payload = JSON.stringify({
    userName: user ? user.userName : ('login_test_' + __VU + '_' + __ITER),
    passWord: user ? user.password : 'loadtest123',
  });

  var params = {
    headers: { 'Content-Type': 'application/json' },
    tags: { name: 'login' },
  };

  var startTime = Date.now();
  var res = http.post(baseUrl + '/user/login', payload, params);
  var duration = Date.now() - startTime;

  loginDuration.add(duration);

  var success = check(res, {
    'status is 200': function (r) { return r.status === 200; },
    'login successful': function (r) {
      try {
        var body = JSON.parse(r.body);
        return body.code === 0;
      } catch (e) {
        return false;
      }
    },
  });

  loginSuccessRate.add(success);

  sleep(0.5 + Math.random() * 0.5);
}

// Generate reports
export function handleSummary(data) {
  return {
    '/reports/user-login.html': htmlReport(data),
    '/reports/user-login.json': JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
  };
}
