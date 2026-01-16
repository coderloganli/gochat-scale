// WebSocket Connection Load Test
// Tests WebSocket /ws endpoint
//
// Usage:
//   K6_VUS=100 K6_DURATION=5m k6 run scenarios/websocket.js
//   K6_START_VUS=10 K6_END_VUS=200 K6_STEP_VUS=20 k6 run scenarios/websocket.js

import ws from 'k6/ws';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';
import { htmlReport } from '../lib/vendor/k6-reporter.js';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.2/index.js';
import { buildStages, getFixedConfig, wsUrl, printConfig } from '../lib/config.js';
import { createTestUsers } from '../lib/auth.js';

// Custom metrics
var wsConnectSuccess = new Rate('ws_connect_success');
var wsConnectDuration = new Trend('ws_connect_duration');
var wsSessionDuration = new Trend('ws_session_duration');
var wsMessagesSent = new Counter('ws_messages_sent');
var wsMessagesReceived = new Counter('ws_messages_received');

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
    websocket_test: scenario,
  },
  thresholds: {
    ws_connect_success: ['rate>0.95'],
    ws_connect_duration: ['p(95)<2000'],
  },
};

// Setup: Create test users
export function setup() {
  printConfig();

  var userCount = useFixedVus ? fixedConfig.vus : parseInt(__ENV.K6_END_VUS) || 100;
  var users = createTestUsers(Math.min(userCount, 100), 'ws_test');

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

  var connectStart = Date.now();
  var sessionStart = null;

  var res = ws.connect(wsUrl, {}, function (socket) {
    var connectDuration = Date.now() - connectStart;
    wsConnectDuration.add(connectDuration);
    sessionStart = Date.now();

    socket.on('open', function () {
      wsConnectSuccess.add(true);

      // Send connection request with auth
      var connectMsg = JSON.stringify({
        authToken: user.authToken,
        roomId: roomId,
      });
      socket.send(connectMsg);
      wsMessagesSent.add(1);
    });

    socket.on('message', function (message) {
      wsMessagesReceived.add(1);
    });

    socket.on('error', function (e) {
      wsConnectSuccess.add(false);
      console.error('WebSocket error: ' + e.error());
    });

    socket.on('close', function () {
      if (sessionStart) {
        wsSessionDuration.add(Date.now() - sessionStart);
      }
    });

    // Keep connection alive for 15-30 seconds
    var connectionTime = 15000 + Math.random() * 15000;
    socket.setTimeout(function () {
      socket.close();
    }, connectionTime);
  });

  var wsConnected = check(res, {
    'WebSocket connection established': function (r) { return r && r.status === 101; },
  });

  if (!wsConnected) {
    wsConnectSuccess.add(false);
  }

  // Small sleep between iterations
  sleep(1);
}

// Generate reports
export function handleSummary(data) {
  return {
    '/reports/websocket.html': htmlReport(data),
    '/reports/websocket.json': JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
  };
}
