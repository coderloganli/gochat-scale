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
  buildStages,
  getFixedConfig,
  baseUrl,
  wsUrl,
  defaultThresholds,
  printConfig,
} from './lib/config.js';
import { createTestUsers, registerUser, loginUser } from './lib/auth.js';
import { randomString, randomInt } from './lib/helpers.js';

// Custom metrics
var overallSuccessRate = new Rate('overall_success_rate');
var httpSuccessRate = new Rate('http_success_rate');
var wsSuccessRate = new Rate('ws_success_rate');
var httpDuration = new Trend('http_request_duration');
var wsConnectDuration = new Trend('ws_connect_duration');
var messagesReceived = new Counter('ws_messages_received');

// Determine test mode based on environment variables
var useFixedVus = __ENV.K6_VUS !== undefined && __ENV.K6_VUS !== '';
var fixedConfig = getFixedConfig();
var stages = buildStages();

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
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<1000'],
    http_req_failed: ['rate<0.05'],
    overall_success_rate: ['rate>0.95'],
    ws_connect_duration: ['p(95)<2000'],
  },
};

// Setup: Create test users
export function setup() {
  printConfig();

  var userCount = useFixedVus ? fixedConfig.vus : parseInt(__ENV.K6_END_VUS) || 100;
  var users = createTestUsers(Math.min(userCount, 200), 'fulltest');

  return { users: users };
}

// HTTP API Scenario
export function httpScenario(data) {
  var headers = { 'Content-Type': 'application/json' };

  group('user_operations', function () {
    // Register new user
    var userName = 'http_' + __VU + '_' + __ITER + '_' + Date.now();
    var password = 'loadtest123';

    var registerPayload = JSON.stringify({ userName: userName, passWord: password });

    var startTime = Date.now();
    var registerRes = http.post(baseUrl + '/user/register', registerPayload, {
      headers: headers,
      tags: { name: 'register' },
    });
    httpDuration.add(Date.now() - startTime);

    var authToken = null;
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

    // If registration failed, try login with existing user
    if (!authToken && data.users.length > 0) {
      var user = data.users[__VU % data.users.length];
      authToken = user.authToken;
    }

    // Authenticated operations
    if (authToken) {
      // Check auth
      var checkPayload = JSON.stringify({ authToken: authToken });
      var checkStart = Date.now();
      var checkRes = http.post(baseUrl + '/user/checkAuth', checkPayload, {
        headers: headers,
        tags: { name: 'checkAuth' },
      });
      httpDuration.add(Date.now() - checkStart);

      var checkSuccess = check(checkRes, {
        'checkAuth: status 200': function (r) { return r.status === 200; },
      });
      httpSuccessRate.add(checkSuccess);
      overallSuccessRate.add(checkSuccess);

      // Get room count
      var countPayload = JSON.stringify({ authToken: authToken, roomId: randomInt(1, 10) });
      var countStart = Date.now();
      var countRes = http.post(baseUrl + '/push/count', countPayload, {
        headers: headers,
        tags: { name: 'pushCount' },
      });
      httpDuration.add(Date.now() - countStart);

      var countSuccess = check(countRes, {
        'pushCount: status 200': function (r) { return r.status === 200; },
      });
      httpSuccessRate.add(countSuccess);
      overallSuccessRate.add(countSuccess);

      // Get room info
      var roomInfoPayload = JSON.stringify({ authToken: authToken, roomId: randomInt(1, 10) });
      var roomInfoStart = Date.now();
      var roomInfoRes = http.post(baseUrl + '/push/getRoomInfo', roomInfoPayload, {
        headers: headers,
        tags: { name: 'getRoomInfo' },
      });
      httpDuration.add(Date.now() - roomInfoStart);

      var roomInfoSuccess = check(roomInfoRes, {
        'getRoomInfo: status 200': function (r) { return r.status === 200; },
      });
      httpSuccessRate.add(roomInfoSuccess);
      overallSuccessRate.add(roomInfoSuccess);
    }
  });

  // Random sleep between iterations
  sleep(0.5 + Math.random() * 1.5);
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
    var connectDuration = Date.now() - startTime;
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
  return {
    '/reports/full-system.html': htmlReport(data),
    '/reports/full-system.json': JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
  };
}
