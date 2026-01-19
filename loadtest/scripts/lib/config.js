// Load Test Configuration - All parameters from environment variables
// No hardcoded values - everything is configurable at runtime

// Base URLs
export const baseUrl = __ENV.API_BASE_URL || 'http://localhost:7070';
export const wsUrl = __ENV.WS_URL || 'ws://localhost:7000/ws';

export function parseDurationToSeconds(value) {
  if (!value) {
    return 0;
  }

  var str = String(value).trim();
  var match = str.match(/^(\d+(?:\.\d+)?)(ms|s|m|h)$/);
  if (!match) {
    return 0;
  }

  var amount = parseFloat(match[1]);
  var unit = match[2];

  if (unit === 'ms') {
    return amount / 1000;
  }
  if (unit === 's') {
    return amount;
  }
  if (unit === 'm') {
    return amount * 60;
  }
  if (unit === 'h') {
    return amount * 3600;
  }
  return 0;
}

// Build ramping stages dynamically from environment variables
export function buildStages() {
  const startVus = parseInt(__ENV.K6_START_VUS) || 10;
  const endVus = parseInt(__ENV.K6_END_VUS) || 100;
  const stepVus = parseInt(__ENV.K6_STEP_VUS) || 10;
  const stepDuration = __ENV.K6_STEP_DURATION || '1m';
  const rampDuration = __ENV.K6_RAMP_DURATION || '30s';

  const stages = [];

  // Generate stages from startVus to endVus with stepVus increments
  for (let vus = startVus; vus <= endVus; vus += stepVus) {
    // Ramp up phase
    stages.push({ duration: rampDuration, target: vus });
    // Stable phase at this level
    stages.push({ duration: stepDuration, target: vus });
  }

  // Cooldown phase
  stages.push({ duration: '30s', target: 0 });

  return stages;
}

export function getCapacityConfig() {
  return {
    startVus: parseInt(__ENV.K6_START_VUS) || 10,
    endVus: parseInt(__ENV.K6_END_VUS) || 100,
    stepVus: parseInt(__ENV.K6_STEP_VUS) || 10,
    stepDuration: __ENV.K6_STEP_DURATION || '1m',
    rampDuration: __ENV.K6_RAMP_DURATION || '30s',
    warmupDuration: __ENV.K6_WARMUP_DURATION || '20s',
  };
}

export function buildCapacityStages() {
  const cfg = getCapacityConfig();
  const stages = [];

  for (let vus = cfg.startVus; vus <= cfg.endVus; vus += cfg.stepVus) {
    stages.push({ duration: cfg.rampDuration, target: vus });
    if (cfg.warmupDuration !== '0s') {
      stages.push({ duration: cfg.warmupDuration, target: vus });
    }
    stages.push({ duration: cfg.stepDuration, target: vus });
  }

  stages.push({ duration: '30s', target: 0 });
  return stages;
}

// Get fixed VUs configuration for constant load tests
export function getFixedConfig() {
  return {
    vus: parseInt(__ENV.K6_VUS) || 100,
    duration: __ENV.K6_DURATION || '5m',
  };
}

// Default thresholds for all tests
export const defaultThresholds = {
  http_req_duration: ['p(95)<500', 'p(99)<1000'],
  http_req_failed: ['rate<0.05'],
};

// Extended thresholds for capacity testing
export const capacityThresholds = {
  http_req_duration: ['p(95)<500', 'p(99)<1000'],
  http_req_failed: ['rate<0.01'],
  'http_req_duration{name:login}': ['p(95)<300'],
  'http_req_duration{name:register}': ['p(95)<300'],
  'http_req_duration{name:checkAuth}': ['p(95)<200'],
  'http_req_duration{name:push}': ['p(95)<500'],
};

// WebSocket thresholds
export const wsThresholds = {
  ws_connecting: ['p(95)<1000'],
  ws_session_duration: ['p(95)<60000'],
};

// Print current configuration (for debugging)
export function printConfig() {
  console.log('=== Load Test Configuration ===');
  console.log(`API Base URL: ${baseUrl}`);
  console.log(`WebSocket URL: ${wsUrl}`);

  if (__ENV.K6_VUS) {
    console.log(`Mode: Fixed VUs`);
    console.log(`VUs: ${__ENV.K6_VUS}`);
    console.log(`Duration: ${__ENV.K6_DURATION || '5m'}`);
  } else {
    console.log(`Mode: Ramping VUs`);
    console.log(`Start VUs: ${__ENV.K6_START_VUS || 10}`);
    console.log(`End VUs: ${__ENV.K6_END_VUS || 100}`);
    console.log(`Step VUs: ${__ENV.K6_STEP_VUS || 10}`);
    console.log(`Step Duration: ${__ENV.K6_STEP_DURATION || '1m'}`);
    console.log(`Ramp Duration: ${__ENV.K6_RAMP_DURATION || '30s'}`);
    console.log(`Warm-up Duration: ${__ENV.K6_WARMUP_DURATION || '20s'}`);
  }
  console.log('================================');
}
