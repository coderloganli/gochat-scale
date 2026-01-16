// Push Message Endpoint Load Test
// Tests POST /push/push endpoint (single user message)
//
// Usage:
//   K6_VUS=100 K6_DURATION=5m k6 run scenarios/push-push.js
//   K6_START_VUS=10 K6_END_VUS=200 K6_STEP_VUS=20 k6 run scenarios/push-push.js

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';
import { htmlReport } from '../lib/vendor/k6-reporter.js';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.2/index.js';
import { buildStages, getFixedConfig, baseUrl, printConfig } from '../lib/config.js';
import { createTestUsers } from '../lib/auth.js';

// Custom metrics
var pushSuccessRate = new Rate('push_success_rate');
var pushDuration = new Trend('push_duration');

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
    push_test: scenario,
  },
  thresholds: {
    http_req_duration: ['p(95)<1000'],
    http_req_failed: ['rate<0.05'],
    push_success_rate: ['rate>0.90'],
  },
};

// Setup: Create test users
export function setup() {
  printConfig();

  var userCount = useFixedVus ? fixedConfig.vus : parseInt(__ENV.K6_END_VUS) || 100;
  var users = createTestUsers(Math.min(userCount, 50), 'push_test');

  return { users: users };
}

// Main test function
export default function (data) {
  if (data.users.length < 2) {
    sleep(1);
    return;
  }

  var fromUserIndex = __VU % data.users.length;
  var fromUser = data.users[fromUserIndex];

  var payload = JSON.stringify({
    authToken: fromUser.authToken,
    msg: 'Load test message ' + __VU + '-' + __ITER + ' at ' + Date.now(),
    toUserId: '1',
    roomId: (__VU % 10) + 1,
  });

  var params = {
    headers: { 'Content-Type': 'application/json' },
    tags: { name: 'push' },
  };

  var startTime = Date.now();
  var res = http.post(baseUrl + '/push/push', payload, params);
  var duration = Date.now() - startTime;

  pushDuration.add(duration);

  var success = check(res, {
    'status is 200': function (r) { return r.status === 200; },
    'push successful': function (r) {
      try {
        var body = JSON.parse(r.body);
        return body.code === 0;
      } catch (e) {
        return false;
      }
    },
  });

  pushSuccessRate.add(success);

  sleep(0.3 + Math.random() * 0.4);
}

// Generate reports
export function handleSummary(data) {
  return {
    '/reports/push-push.html': htmlReport(data),
    '/reports/push-push.json': JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
  };
}
