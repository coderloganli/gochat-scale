// Push Room Message Endpoint Load Test
// Tests POST /push/pushRoom endpoint (room broadcast)
//
// Usage:
//   K6_VUS=100 K6_DURATION=5m k6 run scenarios/push-room.js
//   K6_START_VUS=10 K6_END_VUS=200 K6_STEP_VUS=20 k6 run scenarios/push-room.js

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';
import { htmlReport } from '../lib/vendor/k6-reporter.js';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.2/index.js';
import { buildStages, getFixedConfig, baseUrl, printConfig } from '../lib/config.js';
import { createTestUsers } from '../lib/auth.js';

// Custom metrics
var pushRoomSuccessRate = new Rate('push_room_success_rate');
var pushRoomDuration = new Trend('push_room_duration');

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
    push_room_test: scenario,
  },
  thresholds: {
    http_req_duration: ['p(95)<1000'],
    http_req_failed: ['rate<0.05'],
    push_room_success_rate: ['rate>0.90'],
  },
};

// Setup: Create test users
export function setup() {
  printConfig();

  var userCount = useFixedVus ? fixedConfig.vus : parseInt(__ENV.K6_END_VUS) || 100;
  var users = createTestUsers(Math.min(userCount, 50), 'pushroom_test');

  return { users: users };
}

// Main test function
export default function (data) {
  if (data.users.length === 0) {
    sleep(1);
    return;
  }

  var userIndex = __VU % data.users.length;
  var user = data.users[userIndex];
  var roomId = (__VU % 10) + 1;

  var payload = JSON.stringify({
    authToken: user.authToken,
    msg: 'Room broadcast ' + __VU + '-' + __ITER + ' at ' + Date.now(),
    roomId: roomId,
  });

  var params = {
    headers: { 'Content-Type': 'application/json' },
    tags: { name: 'pushRoom' },
  };

  var startTime = Date.now();
  var res = http.post(baseUrl + '/push/pushRoom', payload, params);
  var duration = Date.now() - startTime;

  pushRoomDuration.add(duration);

  var success = check(res, {
    'status is 200': function (r) { return r.status === 200; },
    'pushRoom successful': function (r) {
      try {
        var body = JSON.parse(r.body);
        return body.code === 0;
      } catch (e) {
        return false;
      }
    },
  });

  pushRoomSuccessRate.add(success);

  sleep(0.3 + Math.random() * 0.4);
}

// Generate reports
export function handleSummary(data) {
  return {
    '/reports/push-room.html': htmlReport(data),
    '/reports/push-room.json': JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
  };
}
