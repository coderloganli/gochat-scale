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
import { buildCapacityStages, getCapacityConfig, getFixedConfig, wsUrl, printConfig } from '../lib/config.js';
import { createTestUsers } from '../lib/auth.js';
import {
  buildRampingStepPlan,
  buildFixedStepPlan,
  createStepTracker,
  buildHttpStepReport,
  buildWsStepThresholds,
} from '../lib/step-report.js';

// Custom metrics
var wsConnectSuccess = new Rate('ws_connect_success');
var wsConnectDuration = new Trend('ws_connect_duration');
var wsSessionDuration = new Trend('ws_session_duration');
var wsMessagesSent = new Counter('ws_messages_sent');
var wsMessagesReceived = new Counter('ws_messages_received');
var steadyWsConnectDuration = new Trend('steady_ws_connect_duration');
var steadyWsConnects = new Counter('steady_ws_connects');
var steadyWsErrors = new Counter('steady_ws_errors');
var steadyWsTimeouts = new Counter('steady_ws_timeouts');
var steadyWs4xx = new Counter('steady_ws_4xx');
var steadyWs5xx = new Counter('steady_ws_5xx');
var steadyWs429 = new Counter('steady_ws_429');
var steadyIters = new Counter('steady_iters');

// Build options
var useFixedVus = __ENV.K6_VUS !== undefined && __ENV.K6_VUS !== '';
var fixedConfig = getFixedConfig();
var capacityConfig = getCapacityConfig();
var stages = buildCapacityStages();
var stepPlan = useFixedVus
  ? buildFixedStepPlan(fixedConfig.duration, capacityConfig.warmupDuration, fixedConfig.vus)
  : buildRampingStepPlan(capacityConfig);
var stepTracker = createStepTracker(stepPlan);
var stepThresholds = buildWsStepThresholds(stepPlan);

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
  thresholds: Object.assign(
    {
      ws_connect_success: ['rate>0.95'],
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
  var users = createTestUsers(Math.min(userCount, 100), 'ws_test');

  return { users: users };
}

// Main test function
export default function (data) {
  var stepInfo = stepTracker.getStepInfo();
  var requestTags = stepTracker.getRequestTags(stepInfo);
  var steadyTags = stepTracker.getSteadyTags(stepInfo);

  if (steadyTags) {
    steadyIters.add(1, steadyTags);
  }

  if (data.users.length === 0) {
    sleep(1);
    return;
  }

  var userIndex = __VU % data.users.length;
  var user = data.users[userIndex];
  var roomId = (__VU % 10) + 1;

  var connectStart = Date.now();
  var sessionStart = null;
  var connectDurationMs = null;
  var errorRecorded = false;

  var res = ws.connect(wsUrl, {}, function (socket) {
    var connectDuration = Math.max(0, Date.now() - connectStart);
    connectDurationMs = connectDuration;
    wsConnectDuration.add(connectDuration, requestTags);
    if (steadyTags) {
      steadyWsConnectDuration.add(connectDuration, steadyTags);
      steadyWsConnects.add(1, steadyTags);
    }
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
      if (steadyTags && !errorRecorded) {
        steadyWsErrors.add(1, steadyTags);
        errorRecorded = true;
      }
      console.error('WebSocket error: ' + e.error());
    });

    socket.on('close', function () {
      if (sessionStart) {
        wsSessionDuration.add(Math.max(0, Date.now() - sessionStart));
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
    if (steadyTags) {
      steadyWsErrors.add(1, steadyTags);
      if (connectDurationMs !== null) {
        steadyWsConnectDuration.add(connectDurationMs, steadyTags);
      }
      steadyWsConnects.add(1, steadyTags);
    }
  }

  // Small sleep between iterations
  sleep(1);
}

// Generate reports
export function handleSummary(data) {
  var stepReport = buildHttpStepReport(data, stepPlan, {
    title: 'WebSocket Step Report',
    latencyMetric: 'steady_ws_connect_duration',
    requestsMetric: 'steady_ws_connects',
    errorsMetric: 'steady_ws_errors',
    timeoutsMetric: 'steady_ws_timeouts',
    http4xxMetric: 'steady_ws_4xx',
    http5xxMetric: 'steady_ws_5xx',
    http429Metric: 'steady_ws_429',
    itersMetric: 'steady_iters',
  });

  return {
    '/reports/websocket.html': stepReport.html,
    '/reports/websocket-steps.json': JSON.stringify(stepReport.json, null, 2),
    '/reports/websocket-full.html': htmlReport(data),
    '/reports/websocket.json': JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
  };
}
