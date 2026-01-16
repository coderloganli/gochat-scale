// Capacity Baseline Test - Step-based load testing
// Dynamically generates stages based on environment variables
//
// Usage:
//   K6_START_VUS=10 K6_END_VUS=500 K6_STEP_VUS=50 k6 run capacity-baseline.js
//
// Environment Variables:
//   K6_START_VUS     - Starting number of VUs (default: 10)
//   K6_END_VUS       - Maximum number of VUs (default: 100)
//   K6_STEP_VUS      - VUs increment per step (default: 10)
//   K6_STEP_DURATION - Duration at each step (default: 1m)
//   K6_RAMP_DURATION - Ramp up duration between steps (default: 30s)

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';
import { htmlReport } from './lib/vendor/k6-reporter.js';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.2/index.js';
import { buildStages, baseUrl, capacityThresholds, printConfig } from './lib/config.js';
import { createTestUsers } from './lib/auth.js';

// Custom metrics for capacity analysis
var requestSuccessRate = new Rate('request_success_rate');
var loginDuration = new Trend('login_duration');
var checkAuthDuration = new Trend('checkauth_duration');
var pushCountDuration = new Trend('push_count_duration');
var totalRequests = new Counter('total_requests');

// Build options dynamically
var stages = buildStages();

export var options = {
  scenarios: {
    capacity_test: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: stages,
      gracefulRampDown: '30s',
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<1000'],
    http_req_failed: ['rate<0.01'],
    'http_req_duration{name:login}': ['p(95)<300'],
    'http_req_duration{name:register}': ['p(95)<300'],
    'http_req_duration{name:checkAuth}': ['p(95)<200'],
    'http_req_duration{name:push}': ['p(95)<500'],
  },
};

// Setup: Create test users
export function setup() {
  printConfig();
  console.log('Generated ' + stages.length + ' stages');
  for (var i = 0; i < stages.length; i++) {
    console.log('  Stage ' + (i + 1) + ': ' + stages[i].duration + ' -> ' + stages[i].target + ' VUs');
  }

  // Create test users for authenticated endpoints
  var userCount = parseInt(__ENV.K6_END_VUS) || 100;
  var users = createTestUsers(Math.min(userCount, 200), 'capacity');

  return { users: users };
}

// Main test function
export default function (data) {
  var headers = { 'Content-Type': 'application/json' };

  // Get user for this VU
  var userIndex = __VU % Math.max(data.users.length, 1);
  var user = data.users.length > 0 ? data.users[userIndex] : null;

  // Test 1: Login endpoint
  var loginUserName = user ? user.userName : ('capacity_login_' + __VU + '_' + __ITER);
  var loginPayload = JSON.stringify({
    userName: loginUserName,
    passWord: user ? user.password : 'loadtest123',
  });

  var startTime = Date.now();
  var res = http.post(baseUrl + '/user/login', loginPayload, {
    headers: headers,
    tags: { name: 'login' },
  });
  var duration = Date.now() - startTime;

  loginDuration.add(duration);
  totalRequests.add(1);

  var success = check(res, {
    'login: status is 200': function (r) { return r.status === 200; },
    'login: response has code': function (r) {
      try {
        var body = JSON.parse(r.body);
        return body.code !== undefined;
      } catch (e) {
        return false;
      }
    },
  });

  requestSuccessRate.add(success);

  // Test 2: CheckAuth endpoint (if we have auth token)
  if (user && user.authToken) {
    var checkPayload = JSON.stringify({
      authToken: user.authToken,
    });

    var checkStartTime = Date.now();
    var checkRes = http.post(baseUrl + '/user/checkAuth', checkPayload, {
      headers: headers,
      tags: { name: 'checkAuth' },
    });
    var checkDuration = Date.now() - checkStartTime;

    checkAuthDuration.add(checkDuration);
    totalRequests.add(1);

    var checkSuccess = check(checkRes, {
      'checkAuth: status is 200': function (r) { return r.status === 200; },
    });

    requestSuccessRate.add(checkSuccess);
  }

  // Test 3: Push count endpoint (if we have auth token)
  if (user && user.authToken) {
    var countPayload = JSON.stringify({
      authToken: user.authToken,
      roomId: (__VU % 10) + 1,
    });

    var countStartTime = Date.now();
    var countRes = http.post(baseUrl + '/push/count', countPayload, {
      headers: headers,
      tags: { name: 'pushCount' },
    });
    var countDuration = Date.now() - countStartTime;

    pushCountDuration.add(countDuration);
    totalRequests.add(1);

    var countSuccess = check(countRes, {
      'pushCount: status is 200': function (r) { return r.status === 200; },
    });

    requestSuccessRate.add(countSuccess);
  }

  // Random sleep between iterations (0.3-0.7 seconds)
  sleep(0.3 + Math.random() * 0.4);
}

// Teardown
export function teardown(data) {
  console.log('Capacity baseline test completed');
  console.log('Total users created: ' + data.users.length);
}

// Generate reports
export function handleSummary(data) {
  return {
    '/reports/capacity-baseline.html': htmlReport(data),
    '/reports/capacity-baseline.json': JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
  };
}
