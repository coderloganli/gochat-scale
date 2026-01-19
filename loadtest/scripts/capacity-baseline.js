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
//   K6_WARMUP_DURATION - Warm-up duration per step (excluded from stats, default: 20s)
//   SLO_P95_MS        - SLO p95 latency threshold in ms (default: 500)
//   SLO_P99_MS        - Optional SLO p99 latency threshold in ms
//   SLO_ERROR_RATE    - SLO error rate threshold (default: 0.01)
//   SLO_TIMEOUT_RATE  - SLO timeout rate threshold (default: 0)

import http from 'k6/http';
import { check, sleep } from 'k6';
import exec from 'k6/execution';
import { Rate, Trend, Counter } from 'k6/metrics';
import { htmlReport } from './lib/vendor/k6-reporter.js';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.2/index.js';
import { buildCapacityStages, getCapacityConfig, parseDurationToSeconds, baseUrl, capacityThresholds, printConfig } from './lib/config.js';
import { buildHttpStepThresholds, buildHttpStepReport } from './lib/step-report.js';
import { createTestUsers } from './lib/auth.js';

// Custom metrics for capacity analysis
var requestSuccessRate = new Rate('request_success_rate');
var loginDuration = new Trend('login_duration');
var checkAuthDuration = new Trend('checkauth_duration');
var pushDuration = new Trend('push_duration');
var pushRoomDuration = new Trend('push_room_duration');
var pushCountDuration = new Trend('push_count_duration');
var roomInfoDuration = new Trend('room_info_duration');
var totalRequests = new Counter('total_requests');

// Per-step steady-state metrics
var steadyHttpDuration = new Trend('steady_http_duration');
var steadyHttpRequests = new Counter('steady_http_requests');
var steadyHttpErrors = new Counter('steady_http_errors');
var steadyHttp4xx = new Counter('steady_http_4xx');
var steadyHttp5xx = new Counter('steady_http_5xx');
var steadyHttp429 = new Counter('steady_http_429');
var steadyHttpTimeout = new Counter('steady_http_timeout');
var steadyIters = new Counter('steady_iters');

// Build options dynamically
var capacityConfig = getCapacityConfig();
var stages = buildCapacityStages();
var stepPlan = buildStepPlan(capacityConfig);
var fallbackStartTime = Date.now();
var stepThresholds = buildHttpStepThresholds(stepPlan);

export var options = {
  scenarios: {
    capacity_test: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: stages,
      gracefulRampDown: '30s',
    },
  },
  thresholds: Object.assign({}, capacityThresholds, stepThresholds),
  summaryTrendStats: ['avg', 'p(90)', 'p(95)', 'p(99)', 'min', 'max'],
};

function buildStepPlan(cfg) {
  var steps = [];
  var rampSeconds = parseDurationToSeconds(cfg.rampDuration);
  var warmupSeconds = parseDurationToSeconds(cfg.warmupDuration);
  var steadySeconds = parseDurationToSeconds(cfg.stepDuration);
  var elapsed = 0;
  var stepIndex = 0;

  for (var vus = cfg.startVus; vus <= cfg.endVus; vus += cfg.stepVus) {
    stepIndex += 1;
    var rampStart = elapsed;
    var warmupStart = rampStart + rampSeconds;
    var steadyStart = warmupStart + warmupSeconds;
    var steadyEnd = steadyStart + steadySeconds;

    steps.push({
      step: stepIndex,
      vus: vus,
      rampStart: rampStart,
      warmupStart: warmupStart,
      steadyStart: steadyStart,
      steadyEnd: steadyEnd,
    });

    elapsed = steadyEnd;
  }

  return {
    steps: steps,
    rampSeconds: rampSeconds,
    warmupSeconds: warmupSeconds,
    steadySeconds: steadySeconds,
    totalSeconds: elapsed,
  };
}

function getElapsedSeconds() {
  var startTime = exec && exec.scenario && exec.scenario.startTime ? exec.scenario.startTime : null;
  var base = startTime || fallbackStartTime;
  // Use Math.max to prevent negative values from clock drift on Windows
  return Math.max(0, Date.now() - base) / 1000;
}

function getStepInfo(elapsedSeconds) {
  for (var i = 0; i < stepPlan.steps.length; i++) {
    var step = stepPlan.steps[i];
    if (elapsedSeconds < step.rampStart) {
      return null;
    }
    if (elapsedSeconds < step.warmupStart) {
      return { step: step.step, vus: step.vus, phase: 'ramp' };
    }
    if (elapsedSeconds < step.steadyStart) {
      return { step: step.step, vus: step.vus, phase: 'warmup' };
    }
    if (elapsedSeconds < step.steadyEnd) {
      return { step: step.step, vus: step.vus, phase: 'steady' };
    }
  }
  return null;
}

function isTimeoutResponse(res) {
  if (!res) {
    return true;
  }
  var errorCode = res.error_code ? String(res.error_code).toLowerCase() : '';
  var errorMsg = res.error ? String(res.error).toLowerCase() : '';
  return errorCode.indexOf('timeout') !== -1 || errorMsg.indexOf('timeout') !== -1;
}

function recordSteadyMetrics(res, duration, stepTags) {
  if (!stepTags) {
    return;
  }

  steadyHttpDuration.add(duration, stepTags);
  steadyHttpRequests.add(1, stepTags);

  var status = res && res.status ? res.status : 0;
  var timeout = isTimeoutResponse(res);
  var failed = timeout || status >= 400;

  if (failed) {
    steadyHttpErrors.add(1, stepTags);
  }
  if (timeout) {
    steadyHttpTimeout.add(1, stepTags);
  }
  if (status === 429) {
    steadyHttp429.add(1, stepTags);
  }
  if (status >= 400 && status < 500) {
    steadyHttp4xx.add(1, stepTags);
  }
  if (status >= 500) {
    steadyHttp5xx.add(1, stepTags);
  }
}

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
  var stepInfo = getStepInfo(getElapsedSeconds());
  var isSteady = stepInfo && stepInfo.phase === 'steady';
  var requestTags = stepInfo
    ? { step: String(stepInfo.step), vus: String(stepInfo.vus), phase: stepInfo.phase }
    : { phase: 'cooldown' };
  var steadyTags = isSteady ? { step: String(stepInfo.step), vus: String(stepInfo.vus) } : null;

  if (isSteady) {
    steadyIters.add(1, steadyTags);
  }

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
    tags: Object.assign({ name: 'login' }, requestTags),
  });
  var duration = Math.max(0, Date.now() - startTime);

  loginDuration.add(duration);
  totalRequests.add(1);
  if (isSteady) {
    recordSteadyMetrics(res, duration, steadyTags);
  }

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
      tags: Object.assign({ name: 'checkAuth' }, requestTags),
    });
    var checkDuration = Math.max(0, Date.now() - checkStartTime);

    checkAuthDuration.add(checkDuration);
    totalRequests.add(1);
    if (isSteady) {
      recordSteadyMetrics(checkRes, checkDuration, steadyTags);
    }

    var checkSuccess = check(checkRes, {
      'checkAuth: status is 200': function (r) { return r.status === 200; },
    });

    requestSuccessRate.add(checkSuccess);
  }

  // Test 3: Push single message (3 times - high frequency operation)
  if (user && user.authToken) {
    var roomId = (__VU % 10) + 1;

    for (var i = 0; i < 3; i++) {
      var pushPayload = JSON.stringify({
        authToken: user.authToken,
        msg: 'capacity test message ' + Date.now(),
        toUserId: __VU,
        roomId: roomId,
      });

      var pushStartTime = Date.now();
      var pushRes = http.post(baseUrl + '/push/push', pushPayload, {
        headers: headers,
        tags: Object.assign({ name: 'push' }, requestTags),
      });
      var pushDur = Math.max(0, Date.now() - pushStartTime);

      pushDuration.add(pushDur);
      totalRequests.add(1);
      if (isSteady) {
        recordSteadyMetrics(pushRes, pushDur, steadyTags);
      }

      var pushSuccess = check(pushRes, {
        'push: status is 200': function (r) { return r.status === 200; },
      });

      requestSuccessRate.add(pushSuccess);
    }
  }

  // Test 4: Push room message (3 times - high frequency operation)
  if (user && user.authToken) {
    var roomId = (__VU % 10) + 1;

    for (var j = 0; j < 3; j++) {
      var pushRoomPayload = JSON.stringify({
        authToken: user.authToken,
        msg: 'capacity room message ' + Date.now(),
        roomId: roomId,
      });

      var pushRoomStartTime = Date.now();
      var pushRoomRes = http.post(baseUrl + '/push/pushRoom', pushRoomPayload, {
        headers: headers,
        tags: Object.assign({ name: 'pushRoom' }, requestTags),
      });
      var pushRoomDur = Math.max(0, Date.now() - pushRoomStartTime);

      pushRoomDuration.add(pushRoomDur);
      totalRequests.add(1);
      if (isSteady) {
        recordSteadyMetrics(pushRoomRes, pushRoomDur, steadyTags);
      }

      var pushRoomSuccess = check(pushRoomRes, {
        'pushRoom: status is 200': function (r) { return r.status === 200; },
      });

      requestSuccessRate.add(pushRoomSuccess);
    }
  }

  // Test 5: Push count endpoint (if we have auth token)
  if (user && user.authToken) {
    var countPayload = JSON.stringify({
      authToken: user.authToken,
      roomId: (__VU % 10) + 1,
    });

    var countStartTime = Date.now();
    var countRes = http.post(baseUrl + '/push/count', countPayload, {
      headers: headers,
      tags: Object.assign({ name: 'pushCount' }, requestTags),
    });
    var countDuration = Math.max(0, Date.now() - countStartTime);

    pushCountDuration.add(countDuration);
    totalRequests.add(1);
    if (isSteady) {
      recordSteadyMetrics(countRes, countDuration, steadyTags);
    }

    var countSuccess = check(countRes, {
      'pushCount: status is 200': function (r) { return r.status === 200; },
    });

    requestSuccessRate.add(countSuccess);
  }

  // Test 6: Get room info endpoint
  if (user && user.authToken) {
    var roomInfoPayload = JSON.stringify({
      authToken: user.authToken,
      roomId: (__VU % 10) + 1,
    });

    var roomInfoStartTime = Date.now();
    var roomInfoRes = http.post(baseUrl + '/push/getRoomInfo', roomInfoPayload, {
      headers: headers,
      tags: Object.assign({ name: 'getRoomInfo' }, requestTags),
    });
    var roomInfoDur = Math.max(0, Date.now() - roomInfoStartTime);

    roomInfoDuration.add(roomInfoDur);
    totalRequests.add(1);
    if (isSteady) {
      recordSteadyMetrics(roomInfoRes, roomInfoDur, steadyTags);
    }

    var roomInfoSuccess = check(roomInfoRes, {
      'getRoomInfo: status is 200': function (r) { return r.status === 200; },
    });

    requestSuccessRate.add(roomInfoSuccess);
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
  var stepReport = buildHttpStepReport(data, stepPlan, { title: 'Capacity Baseline Step Report' });

  return {
    '/reports/capacity-baseline.html': stepReport.html,
    '/reports/capacity-baseline-steps.json': JSON.stringify(stepReport.json, null, 2),
    '/reports/capacity-baseline-full.html': htmlReport(data),
    '/reports/capacity-baseline.json': JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
  };
}
