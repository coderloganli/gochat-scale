// Full System Load Test
// Tests all HTTP endpoints and WebSocket connections simultaneously
//
// Usage:
//   K6_VUS=100 K6_DURATION=5m k6 run full-system.js
//
// Or with ramping stages:
//   K6_START_VUS=10 K6_END_VUS=200 k6 run full-system.js

import http from 'k6/http';
import ws from 'k6/ws';
import { check, sleep, group } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';
import { htmlReport } from './lib/vendor/k6-reporter.js';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.2/index.js';
import {
  buildCapacityStages,
  getCapacityConfig,
  getFixedConfig,
  baseUrl,
  wsUrl,
  defaultThresholds,
  printConfig,
} from './lib/config.js';
import { createTestUsers, registerUser, loginUser } from './lib/auth.js';
import { randomString, randomInt } from './lib/helpers.js';
import {
  buildRampingStepPlan,
  buildFixedStepPlan,
  createStepTracker,
  recordHttpSteadyMetrics,
  buildHttpStepReport,
  buildHttpStepThresholds,
} from './lib/step-report.js';

// Custom metrics
var overallSuccessRate = new Rate('overall_success_rate');
var httpSuccessRate = new Rate('http_success_rate');
var wsSuccessRate = new Rate('ws_success_rate');
var httpDuration = new Trend('http_request_duration');
var wsConnectDuration = new Trend('ws_connect_duration');
var messagesReceived = new Counter('ws_messages_received');
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

// Determine test mode based on environment variables
var useFixedVus = __ENV.K6_VUS !== undefined && __ENV.K6_VUS !== '';
var fixedConfig = getFixedConfig();
var capacityConfig = getCapacityConfig();
var stages = buildCapacityStages();
var stepPlan = useFixedVus
  ? buildFixedStepPlan(fixedConfig.duration, capacityConfig.warmupDuration, fixedConfig.vus)
  : buildRampingStepPlan(capacityConfig);
var stepTracker = createStepTracker(stepPlan);
var stepThresholds = buildHttpStepThresholds(stepPlan);

// Build WebSocket stages (25% of HTTP VUs)
function buildWsStages(httpStages) {
  var wsStages = [];
  for (var i = 0; i < httpStages.length; i++) {
    wsStages.push({
      duration: httpStages[i].duration,
      target: Math.floor(httpStages[i].target / 4)
    });
  }
  return wsStages;
}

// Build options based on mode
var scenarios = {};

if (useFixedVus) {
  // Fixed VUs mode
  scenarios = {
    http_load: {
      executor: 'constant-vus',
      vus: fixedConfig.vus,
      duration: fixedConfig.duration,
      exec: 'httpScenario',
    },
    websocket_load: {
      executor: 'constant-vus',
      vus: Math.floor(fixedConfig.vus / 4),
      duration: fixedConfig.duration,
      exec: 'wsScenario',
    },
  };
} else {
  // Ramping VUs mode
  scenarios = {
    http_load: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: stages,
      exec: 'httpScenario',
      gracefulRampDown: '30s',
    },
    websocket_load: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: buildWsStages(stages),
      exec: 'wsScenario',
      gracefulRampDown: '30s',
    },
  };
}

export var options = {
  scenarios: scenarios,
  thresholds: Object.assign(
    {
      http_req_duration: ['p(95)<500', 'p(99)<1000'],
      http_req_failed: ['rate<0.05'],
      overall_success_rate: ['rate>0.95'],
      ws_connect_duration: ['p(95)<2000'],
    },
    stepThresholds
  ),
  summaryTrendStats: ['avg', 'p(90)', 'p(95)', 'p(99)', 'min', 'max'],
};

// Setup: Create test users
export function setup() {
  printConfig();

  var userCount = useFixedVus ? fixedConfig.vus : parseInt(__ENV.K6_END_VUS) || 100;
  var users = createTestUsers(Math.min(userCount, 200), 'fulltest');

  return { users: users };
}

// HTTP API Scenario
// Request ratio per iteration (10:2:1):
//   - Message sending (push + pushRoom): 10 times (5 + 5)
//   - Room count query: 2 times
//   - Other operations (checkAuth + getRoomInfo): 1 time each
export function httpScenario(data) {
  var headers = { 'Content-Type': 'application/json' };
  var stepInfo = stepTracker.getStepInfo();
  var requestTags = stepTracker.getRequestTags(stepInfo);
  var steadyTags = stepTracker.getSteadyTags(stepInfo);

  if (steadyTags) {
    steadyIters.add(1, steadyTags);
  }

  // Get auth token from pre-created users
  var authToken = null;
  var userId = null;

  if (data.users.length > 0) {
    var user = data.users[__VU % data.users.length];
    authToken = user.authToken;
  }

  // If no pre-created user, register a new one
  if (!authToken) {
    group('user_registration', function () {
      var userName = 'http_' + __VU + '_' + __ITER + '_' + Date.now();
      var password = 'loadtest123';

      var registerPayload = JSON.stringify({ userName: userName, passWord: password });

      var startTime = Date.now();
      var registerRes = http.post(baseUrl + '/user/register', registerPayload, {
        headers: headers,
        tags: Object.assign({ name: 'register' }, requestTags),
      });
      var registerDurationMs = Math.max(0, Date.now() - startTime);
      httpDuration.add(registerDurationMs);
      recordHttpSteadyMetrics(registerRes, registerDurationMs, steadyTags, steadyMetrics);

      var registerSuccess = check(registerRes, {
        'register: status 200': function (r) { return r.status === 200; },
      });
      httpSuccessRate.add(registerSuccess);
      overallSuccessRate.add(registerSuccess);

      if (registerRes.status === 200) {
        try {
          var body = JSON.parse(registerRes.body);
          if (body.code === 0) {
            authToken = body.data;
          }
        } catch (e) {}
      }
    });
  }

  // Authenticated operations
  if (authToken) {
    // 1. Check auth (1 time) - low frequency
    group('auth_check', function () {
      var checkPayload = JSON.stringify({ authToken: authToken });
      var checkStart = Date.now();
      var checkRes = http.post(baseUrl + '/user/checkAuth', checkPayload, {
        headers: headers,
        tags: Object.assign({ name: 'checkAuth' }, requestTags),
      });
      var checkDurationMs = Math.max(0, Date.now() - checkStart);
      httpDuration.add(checkDurationMs);
      recordHttpSteadyMetrics(checkRes, checkDurationMs, steadyTags, steadyMetrics);

      var checkSuccess = check(checkRes, {
        'checkAuth: status 200': function (r) { return r.status === 200; },
      });
      httpSuccessRate.add(checkSuccess);
      overallSuccessRate.add(checkSuccess);

      // Extract userId for push operations
      if (checkRes.status === 200) {
        try {
          var body = JSON.parse(checkRes.body);
          if (body.code === 0 && body.data) {
            userId = body.data.userId;
          }
        } catch (e) {}
      }
    });

    // 2. Message sending - HIGH FREQUENCY (10 times total)
    group('message_sending', function () {
      var roomId = randomInt(1, 10);

      // Push single messages (5 times)
      for (var i = 0; i < 5; i++) {
        var targetUserId = userId || randomInt(1, 100);
        var pushPayload = JSON.stringify({
          authToken: authToken,
          msg: 'test message ' + Date.now(),
          toUserId: targetUserId,
          roomId: roomId,
        });

        var pushStart = Date.now();
        var pushRes = http.post(baseUrl + '/push/push', pushPayload, {
          headers: headers,
          tags: Object.assign({ name: 'push' }, requestTags),
        });
        var pushDurationMs = Math.max(0, Date.now() - pushStart);
        httpDuration.add(pushDurationMs);
        recordHttpSteadyMetrics(pushRes, pushDurationMs, steadyTags, steadyMetrics);

        var pushSuccess = check(pushRes, {
          'push: status 200': function (r) { return r.status === 200; },
        });
        httpSuccessRate.add(pushSuccess);
        overallSuccessRate.add(pushSuccess);

        sleep(0.05 + Math.random() * 0.1); // Short delay between messages
      }

      // Push room messages (5 times)
      for (var j = 0; j < 5; j++) {
        var pushRoomPayload = JSON.stringify({
          authToken: authToken,
          msg: 'room message ' + Date.now(),
          roomId: roomId,
        });

        var pushRoomStart = Date.now();
        var pushRoomRes = http.post(baseUrl + '/push/pushRoom', pushRoomPayload, {
          headers: headers,
          tags: Object.assign({ name: 'pushRoom' }, requestTags),
        });
        var pushRoomDurationMs = Math.max(0, Date.now() - pushRoomStart);
        httpDuration.add(pushRoomDurationMs);
        recordHttpSteadyMetrics(pushRoomRes, pushRoomDurationMs, steadyTags, steadyMetrics);

        var pushRoomSuccess = check(pushRoomRes, {
          'pushRoom: status 200': function (r) { return r.status === 200; },
        });
        httpSuccessRate.add(pushRoomSuccess);
        overallSuccessRate.add(pushRoomSuccess);

        sleep(0.05 + Math.random() * 0.1); // Short delay between messages
      }
    });

    // 3. Room count query - MEDIUM FREQUENCY (2 times)
    group('room_queries', function () {
      for (var k = 0; k < 2; k++) {
        var countPayload = JSON.stringify({ authToken: authToken, roomId: randomInt(1, 10) });
        var countStart = Date.now();
        var countRes = http.post(baseUrl + '/push/count', countPayload, {
          headers: headers,
          tags: Object.assign({ name: 'pushCount' }, requestTags),
        });
        var countDurationMs = Math.max(0, Date.now() - countStart);
        httpDuration.add(countDurationMs);
        recordHttpSteadyMetrics(countRes, countDurationMs, steadyTags, steadyMetrics);

        var countSuccess = check(countRes, {
          'pushCount: status 200': function (r) { return r.status === 200; },
        });
        httpSuccessRate.add(countSuccess);
        overallSuccessRate.add(countSuccess);

        sleep(0.1);
      }
    });

    // 4. Get room info - LOW FREQUENCY (1 time)
    group('room_info', function () {
      var roomInfoPayload = JSON.stringify({ authToken: authToken, roomId: randomInt(1, 10) });
      var roomInfoStart = Date.now();
      var roomInfoRes = http.post(baseUrl + '/push/getRoomInfo', roomInfoPayload, {
        headers: headers,
        tags: Object.assign({ name: 'getRoomInfo' }, requestTags),
      });
      var roomInfoDurationMs = Math.max(0, Date.now() - roomInfoStart);
      httpDuration.add(roomInfoDurationMs);
      recordHttpSteadyMetrics(roomInfoRes, roomInfoDurationMs, steadyTags, steadyMetrics);

      var roomInfoSuccess = check(roomInfoRes, {
        'getRoomInfo: status 200': function (r) { return r.status === 200; },
      });
      httpSuccessRate.add(roomInfoSuccess);
      overallSuccessRate.add(roomInfoSuccess);
    });
  }

  // Random sleep between iterations
  sleep(0.3 + Math.random() * 0.7);
}

// Default function (fallback when scenarios not used)
export default function (data) {
  httpScenario(data);
}

// WebSocket Scenario
export function wsScenario(data) {
  if (data.users.length === 0) {
    sleep(1);
    return;
  }

  var user = data.users[__VU % data.users.length];
  var roomId = randomInt(1, 10);

  var startTime = Date.now();

  var res = ws.connect(wsUrl, {}, function (socket) {
    var connectDuration = Math.max(0, Date.now() - startTime);
    wsConnectDuration.add(connectDuration);

    socket.on('open', function () {
      wsSuccessRate.add(true);
      overallSuccessRate.add(true);

      // Send connection request with auth
      var connectMsg = JSON.stringify({
        authToken: user.authToken,
        roomId: roomId,
      });
      socket.send(connectMsg);
    });

    socket.on('message', function (message) {
      messagesReceived.add(1);
    });

    socket.on('error', function (e) {
      wsSuccessRate.add(false);
      overallSuccessRate.add(false);
      console.error('WebSocket error: ' + e.error());
    });

    socket.on('close', function () {
      // Connection closed normally
    });

    // Keep connection alive for 20-40 seconds
    var connectionTime = randomInt(20000, 40000);
    socket.setTimeout(function () {
      socket.close();
    }, connectionTime);
  });

  var wsConnected = check(res, {
    'WebSocket: connection established': function (r) { return r && r.status === 101; },
  });

  if (!wsConnected) {
    wsSuccessRate.add(false);
    overallSuccessRate.add(false);
  }
}

// Teardown
export function teardown(data) {
  console.log('Full system load test completed');
  console.log('Total users: ' + data.users.length);
}

// Generate reports
export function handleSummary(data) {
  var stepReport = buildHttpStepReport(data, stepPlan, { title: 'Full System Step Report' });

  return {
    '/reports/full-system.html': stepReport.html,
    '/reports/full-system-steps.json': JSON.stringify(stepReport.json, null, 2),
    '/reports/full-system-full.html': htmlReport(data),
    '/reports/full-system.json': JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
  };
}
