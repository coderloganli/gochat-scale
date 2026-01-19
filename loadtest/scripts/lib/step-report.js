import exec from 'k6/execution';
import { parseDurationToSeconds } from './config.js';

export function buildRampingStepPlan(cfg) {
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
      steadySeconds: steadySeconds,
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

export function buildFixedStepPlan(duration, warmupDuration, vus) {
  var totalSeconds = parseDurationToSeconds(duration);
  var warmupSeconds = parseDurationToSeconds(warmupDuration);
  var steadySeconds = Math.max(totalSeconds - warmupSeconds, 0);

  return {
    steps: [
      {
        step: 1,
        vus: vus,
        rampStart: 0,
        warmupStart: 0,
        steadyStart: warmupSeconds,
        steadyEnd: totalSeconds,
        steadySeconds: steadySeconds,
      },
    ],
    rampSeconds: 0,
    warmupSeconds: warmupSeconds,
    steadySeconds: steadySeconds,
    totalSeconds: totalSeconds,
  };
}

export function createStepTracker(stepPlan) {
  var fallbackStartTime = Date.now();

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

  function getRequestTags(stepInfo) {
    if (!stepInfo) {
      return { phase: 'cooldown' };
    }
    return { step: String(stepInfo.step), vus: String(stepInfo.vus), phase: stepInfo.phase };
  }

  function getSteadyTags(stepInfo) {
    if (!stepInfo || stepInfo.phase !== 'steady') {
      return null;
    }
    return { step: String(stepInfo.step), vus: String(stepInfo.vus) };
  }

  return {
    stepPlan: stepPlan,
    getStepInfo: function () {
      return getStepInfo(getElapsedSeconds());
    },
    getRequestTags: getRequestTags,
    getSteadyTags: getSteadyTags,
  };
}

export function isTimeoutResponse(res) {
  if (!res) {
    return true;
  }
  var errorCode = res.error_code ? String(res.error_code).toLowerCase() : '';
  var errorMsg = res.error ? String(res.error).toLowerCase() : '';
  return errorCode.indexOf('timeout') !== -1 || errorMsg.indexOf('timeout') !== -1;
}

export function recordHttpSteadyMetrics(res, duration, steadyTags, metrics) {
  if (!steadyTags) {
    return;
  }

  metrics.duration.add(duration, steadyTags);
  metrics.requests.add(1, steadyTags);

  var status = res && res.status ? res.status : 0;
  var timeout = isTimeoutResponse(res);
  var failed = timeout || status >= 400;

  if (failed) {
    metrics.errors.add(1, steadyTags);
  }
  if (timeout) {
    metrics.timeouts.add(1, steadyTags);
  }
  if (status === 429) {
    metrics.http429.add(1, steadyTags);
  }
  if (status >= 400 && status < 500) {
    metrics.http4xx.add(1, steadyTags);
  }
  if (status >= 500) {
    metrics.http5xx.add(1, steadyTags);
  }
}

export function buildStepThresholds(stepPlan, metricRules) {
  var thresholds = {};
  var steps = stepPlan && stepPlan.steps ? stepPlan.steps : [];
  if (!metricRules || metricRules.length === 0 || steps.length === 0) {
    return thresholds;
  }

  for (var i = 0; i < steps.length; i++) {
    var step = steps[i];
    for (var j = 0; j < metricRules.length; j++) {
      var rule = metricRules[j];
      var name = rule.name;
      var threshold = rule.threshold;
      if (!name || !threshold) {
        continue;
      }
      var key = name + '{step:' + step.step + ',vus:' + step.vus + '}';
      thresholds[key] = [threshold];
    }
  }

  return thresholds;
}

export function buildHttpStepThresholds(stepPlan) {
  return buildStepThresholds(stepPlan, [
    { name: 'steady_http_duration', threshold: 'p(95)>=0' },
    { name: 'steady_http_requests', threshold: 'count>=0' },
    { name: 'steady_http_errors', threshold: 'count>=0' },
    { name: 'steady_http_timeout', threshold: 'count>=0' },
    { name: 'steady_http_4xx', threshold: 'count>=0' },
    { name: 'steady_http_5xx', threshold: 'count>=0' },
    { name: 'steady_http_429', threshold: 'count>=0' },
    { name: 'steady_iters', threshold: 'count>=0' },
  ]);
}

export function buildWsStepThresholds(stepPlan) {
  return buildStepThresholds(stepPlan, [
    { name: 'steady_ws_connect_duration', threshold: 'p(95)>=0' },
    { name: 'steady_ws_connects', threshold: 'count>=0' },
    { name: 'steady_ws_errors', threshold: 'count>=0' },
    { name: 'steady_ws_timeouts', threshold: 'count>=0' },
    { name: 'steady_ws_4xx', threshold: 'count>=0' },
    { name: 'steady_ws_5xx', threshold: 'count>=0' },
    { name: 'steady_ws_429', threshold: 'count>=0' },
    { name: 'steady_iters', threshold: 'count>=0' },
  ]);
}

function extractTrend(metrics, name) {
  var out = {};

  // First try submetrics (older k6 format)
  var metric = metrics[name];
  if (metric && metric.submetrics) {
    var keys = Object.keys(metric.submetrics);
    for (var i = 0; i < keys.length; i++) {
      var sub = metric.submetrics[keys[i]];
      var tags = sub.tags || {};
      if (!tags.step || !tags.vus) {
        continue;
      }
      var key = tags.step + '|' + tags.vus;
      out[key] = sub.values || {};
    }
  }

  // Also scan top-level metrics for tagged format: name{step:X,vus:Y}
  var prefix = name + '{';
  var metricKeys = Object.keys(metrics);
  for (var j = 0; j < metricKeys.length; j++) {
    var metricKey = metricKeys[j];
    if (metricKey.indexOf(prefix) !== 0) {
      continue;
    }
    // Parse tags from key like "steady_http_duration{step:1,vus:5}"
    var tagPart = metricKey.slice(prefix.length, -1); // "step:1,vus:5"
    var tagPairs = tagPart.split(',');
    var step = null;
    var vus = null;
    for (var k = 0; k < tagPairs.length; k++) {
      var pair = tagPairs[k].split(':');
      if (pair[0] === 'step') step = pair[1];
      if (pair[0] === 'vus') vus = pair[1];
    }
    if (step && vus) {
      var outKey = step + '|' + vus;
      var m = metrics[metricKey];
      if (m && m.values) {
        out[outKey] = m.values;
      }
    }
  }

  return out;
}

function extractCount(metrics, name) {
  var out = {};

  // First try submetrics (older k6 format)
  var metric = metrics[name];
  if (metric && metric.submetrics) {
    var keys = Object.keys(metric.submetrics);
    for (var i = 0; i < keys.length; i++) {
      var sub = metric.submetrics[keys[i]];
      var tags = sub.tags || {};
      if (!tags.step || !tags.vus) {
        continue;
      }
      var key = tags.step + '|' + tags.vus;
      out[key] = sub.values && sub.values.count ? sub.values.count : 0;
    }
  }

  // Also scan top-level metrics for tagged format: name{step:X,vus:Y}
  var prefix = name + '{';
  var metricKeys = Object.keys(metrics);
  for (var j = 0; j < metricKeys.length; j++) {
    var metricKey = metricKeys[j];
    if (metricKey.indexOf(prefix) !== 0) {
      continue;
    }
    // Parse tags from key like "steady_http_requests{step:1,vus:5}"
    var tagPart = metricKey.slice(prefix.length, -1);
    var tagPairs = tagPart.split(',');
    var step = null;
    var vus = null;
    for (var k = 0; k < tagPairs.length; k++) {
      var pair = tagPairs[k].split(':');
      if (pair[0] === 'step') step = pair[1];
      if (pair[0] === 'vus') vus = pair[1];
    }
    if (step && vus) {
      var outKey = step + '|' + vus;
      var m = metrics[metricKey];
      if (m && m.values && m.values.count !== undefined) {
        out[outKey] = m.values.count;
      }
    }
  }

  return out;
}

function formatNumber(value, digits) {
  if (value === null || value === undefined || isNaN(value)) {
    return '-';
  }
  var fixed = digits !== undefined ? value.toFixed(digits) : value.toFixed(2);
  return fixed;
}

function formatRate(value) {
  if (value === null || value === undefined || isNaN(value)) {
    return '-';
  }
  return (value * 100).toFixed(2) + '%';
}

function renderStepHtml(rows, capacity, bottleneck, slo, title) {
  var html = [];
  html.push('<!doctype html>');
  html.push('<html lang="en"><head><meta charset="utf-8">');
  html.push('<meta name="viewport" content="width=device-width, initial-scale=1">');
  html.push('<title>' + title + '</title>');
  html.push('<script src="https://cdn.jsdelivr.net/npm/chart.js"><\/script>');
  html.push('<style>');
  html.push('body{font-family:Arial,Helvetica,sans-serif;margin:24px;color:#222;background:#f8f9fb}');
  html.push('h1{margin:0 0 8px 0;font-size:22px}');
  html.push('h2{margin:24px 0 12px 0;font-size:18px;color:#333}');
  html.push('.meta{margin-bottom:16px;font-size:13px;color:#555}');
  html.push('.card{background:#fff;border:1px solid #e3e6ea;border-radius:8px;padding:16px;margin-bottom:16px}');
  html.push('.summary-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:16px;margin-bottom:16px}');
  html.push('.summary-card{background:#fff;border:1px solid #e3e6ea;border-radius:8px;padding:16px}');
  html.push('.summary-card.capacity{border-left:4px solid #0a7d37}');
  html.push('.summary-card.bottleneck{border-left:4px solid #b00020}');
  html.push('.summary-card h3{margin:0 0 8px 0;font-size:14px;color:#666}');
  html.push('.summary-card .value{font-size:24px;font-weight:bold;color:#222}');
  html.push('.summary-card .detail{font-size:12px;color:#666;margin-top:4px}');
  html.push('.summary-card .reasons{font-size:12px;color:#b00020;margin-top:8px}');
  html.push('.chart-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(400px,1fr));gap:16px;margin-bottom:16px}');
  html.push('.chart-card{background:#fff;border:1px solid #e3e6ea;border-radius:8px;padding:16px}');
  html.push('.chart-card h3{margin:0 0 12px 0;font-size:14px;color:#666}');
  html.push('table{width:100%;border-collapse:collapse;font-size:12px}');
  html.push('th,td{border:1px solid #e3e6ea;padding:6px 8px;text-align:right}');
  html.push('th{text-align:center;background:#f1f3f6}');
  html.push('td.left{text-align:left}');
  html.push('.pass{color:#0a7d37;font-weight:bold}');
  html.push('.fail{color:#b00020;font-weight:bold}');
  html.push('.row-fail{background:#fff5f5}');
  html.push('</style></head><body>');
  html.push('<h1>' + title + '</h1>');
  html.push('<div class="meta">Steady-state only. Warm-up excluded. Per-step throughput, errors, and latency percentiles.</div>');

  // SLO card
  html.push('<div class="card"><strong>SLO Thresholds</strong><div class="meta">');
  html.push('p95 ≤ ' + slo.sloP95 + ' ms');
  if (slo.sloP99 !== null) {
    html.push(', p99 ≤ ' + slo.sloP99 + ' ms');
  }
  html.push(', error rate ≤ ' + (slo.sloErrorRate * 100).toFixed(2) + '%');
  html.push(', timeout rate ≤ ' + (slo.sloTimeoutRate * 100).toFixed(2) + '%');
  html.push('</div></div>');

  // Summary cards
  html.push('<div class="summary-grid">');

  // Capacity card
  html.push('<div class="summary-card capacity">');
  html.push('<h3>Maximum Capacity (Last SLO-Passing Step)</h3>');
  if (capacity) {
    html.push('<div class="value">' + capacity.vus + ' VUs</div>');
    html.push('<div class="detail">Step ' + capacity.step + ' | RPS: ' + formatNumber(capacity.rps, 2) + ' | Iter/s: ' + formatNumber(capacity.itersPerSec, 2) + '</div>');
  } else {
    html.push('<div class="value">N/A</div>');
    html.push('<div class="detail">No step met SLO</div>');
  }
  html.push('</div>');

  // Bottleneck card
  html.push('<div class="summary-card bottleneck">');
  html.push('<h3>Bottleneck (First SLO-Failing Step)</h3>');
  if (bottleneck) {
    html.push('<div class="value">' + bottleneck.vus + ' VUs</div>');
    html.push('<div class="detail">Step ' + bottleneck.step + ' | RPS: ' + formatNumber(bottleneck.rps, 2) + '</div>');
    if (bottleneck.reasons && bottleneck.reasons.length > 0) {
      html.push('<div class="reasons"><strong>Reasons:</strong><ul style="margin:4px 0;padding-left:20px">');
      for (var r = 0; r < bottleneck.reasons.length; r++) {
        html.push('<li>' + bottleneck.reasons[r] + '</li>');
      }
      html.push('</ul></div>');
    }
  } else {
    html.push('<div class="value">None</div>');
    html.push('<div class="detail">All steps passed SLO</div>');
  }
  html.push('</div>');
  html.push('</div>');

  // Charts
  html.push('<h2>Performance Charts</h2>');
  html.push('<div class="chart-grid">');

  html.push('<div class="chart-card"><h3>Throughput vs VUs</h3><canvas id="throughputChart"></canvas></div>');
  html.push('<div class="chart-card"><h3>Latency vs VUs</h3><canvas id="latencyChart"></canvas></div>');
  html.push('<div class="chart-card"><h3>Error Rate vs VUs</h3><canvas id="errorChart"></canvas></div>');

  html.push('</div>');

  // Data table
  html.push('<h2>Step Details</h2>');
  html.push('<div class="card">');
  html.push('<table>');
  html.push('<thead><tr>');
  html.push('<th>Step</th>');
  html.push('<th>VUs</th>');
  html.push('<th>RPS</th>');
  html.push('<th>Iter/s</th>');
  html.push('<th>Error Rate</th>');
  html.push('<th>Timeout Rate</th>');
  html.push('<th>p90</th>');
  html.push('<th>p95</th>');
  html.push('<th>p99</th>');
  html.push('<th>4xx</th>');
  html.push('<th>5xx</th>');
  html.push('<th>429</th>');
  html.push('<th>Timeouts</th>');
  html.push('<th>SLO</th>');
  html.push('</tr></thead><tbody>');

  for (var i = 0; i < rows.length; i++) {
    var row = rows[i];
    var rowClass = row.sloPass ? '' : ' class="row-fail"';
    html.push('<tr' + rowClass + '>');
    html.push('<td class="left">' + row.step + '</td>');
    html.push('<td>' + row.vus + '</td>');
    html.push('<td>' + formatNumber(row.rps, 2) + '</td>');
    html.push('<td>' + formatNumber(row.itersPerSec, 2) + '</td>');
    html.push('<td>' + formatRate(row.errorRate) + '</td>');
    html.push('<td>' + formatRate(row.timeoutRate) + '</td>');
    html.push('<td>' + formatNumber(row.p90, 2) + '</td>');
    html.push('<td>' + formatNumber(row.p95, 2) + '</td>');
    html.push('<td>' + formatNumber(row.p99, 2) + '</td>');
    html.push('<td>' + row.http4xx + '</td>');
    html.push('<td>' + row.http5xx + '</td>');
    html.push('<td>' + row.http429 + '</td>');
    html.push('<td>' + row.timeouts + '</td>');
    html.push('<td class="' + (row.sloPass ? 'pass' : 'fail') + '">' + (row.sloPass ? 'PASS' : 'FAIL') + '</td>');
    html.push('</tr>');
  }

  html.push('</tbody></table></div>');

  // Chart scripts
  html.push('<script>');
  html.push('var chartData = ' + JSON.stringify(rows.map(function(r) {
    return {
      vus: r.vus,
      rps: r.rps || 0,
      itersPerSec: r.itersPerSec || 0,
      p90: r.p90 || 0,
      p95: r.p95 || 0,
      p99: r.p99 || 0,
      errorRate: (r.errorRate || 0) * 100,
      timeoutRate: (r.timeoutRate || 0) * 100,
      sloPass: r.sloPass
    };
  })) + ';');

  html.push('var labels = chartData.map(function(r) { return r.vus + " VUs"; });');
  html.push('var passColor = "#0a7d37";');
  html.push('var failColor = "#b00020";');
  html.push('var pointColors = chartData.map(function(r) { return r.sloPass ? passColor : failColor; });');

  // Throughput chart
  html.push('new Chart(document.getElementById("throughputChart"), {');
  html.push('  type: "line",');
  html.push('  data: {');
  html.push('    labels: labels,');
  html.push('    datasets: [{');
  html.push('      label: "RPS",');
  html.push('      data: chartData.map(function(r) { return r.rps; }),');
  html.push('      borderColor: "#2196F3",');
  html.push('      backgroundColor: "rgba(33, 150, 243, 0.1)",');
  html.push('      pointBackgroundColor: pointColors,');
  html.push('      pointBorderColor: pointColors,');
  html.push('      pointRadius: 6,');
  html.push('      fill: true,');
  html.push('      tension: 0.3');
  html.push('    }, {');
  html.push('      label: "Iter/s",');
  html.push('      data: chartData.map(function(r) { return r.itersPerSec; }),');
  html.push('      borderColor: "#9C27B0",');
  html.push('      backgroundColor: "rgba(156, 39, 176, 0.1)",');
  html.push('      pointBackgroundColor: pointColors,');
  html.push('      pointBorderColor: pointColors,');
  html.push('      pointRadius: 6,');
  html.push('      fill: true,');
  html.push('      tension: 0.3');
  html.push('    }]');
  html.push('  },');
  html.push('  options: { responsive: true, plugins: { legend: { position: "top" } } }');
  html.push('});');

  // Latency chart
  html.push('new Chart(document.getElementById("latencyChart"), {');
  html.push('  type: "line",');
  html.push('  data: {');
  html.push('    labels: labels,');
  html.push('    datasets: [{');
  html.push('      label: "p90 (ms)",');
  html.push('      data: chartData.map(function(r) { return r.p90; }),');
  html.push('      borderColor: "#4CAF50",');
  html.push('      pointBackgroundColor: pointColors,');
  html.push('      pointBorderColor: pointColors,');
  html.push('      pointRadius: 5,');
  html.push('      fill: false,');
  html.push('      tension: 0.3');
  html.push('    }, {');
  html.push('      label: "p95 (ms)",');
  html.push('      data: chartData.map(function(r) { return r.p95; }),');
  html.push('      borderColor: "#FF9800",');
  html.push('      pointBackgroundColor: pointColors,');
  html.push('      pointBorderColor: pointColors,');
  html.push('      pointRadius: 5,');
  html.push('      fill: false,');
  html.push('      tension: 0.3');
  html.push('    }, {');
  html.push('      label: "p99 (ms)",');
  html.push('      data: chartData.map(function(r) { return r.p99; }),');
  html.push('      borderColor: "#F44336",');
  html.push('      pointBackgroundColor: pointColors,');
  html.push('      pointBorderColor: pointColors,');
  html.push('      pointRadius: 5,');
  html.push('      fill: false,');
  html.push('      tension: 0.3');
  html.push('    }]');
  html.push('  },');
  html.push('  options: {');
  html.push('    responsive: true,');
  html.push('    plugins: { legend: { position: "top" } },');
  html.push('    scales: { y: { beginAtZero: true, title: { display: true, text: "Latency (ms)" } } }');
  html.push('  }');
  html.push('});');

  // Error rate chart
  html.push('new Chart(document.getElementById("errorChart"), {');
  html.push('  type: "bar",');
  html.push('  data: {');
  html.push('    labels: labels,');
  html.push('    datasets: [{');
  html.push('      label: "Error Rate (%)",');
  html.push('      data: chartData.map(function(r) { return r.errorRate; }),');
  html.push('      backgroundColor: pointColors.map(function(c) { return c === passColor ? "rgba(10, 125, 55, 0.7)" : "rgba(176, 0, 32, 0.7)"; }),');
  html.push('      borderColor: pointColors,');
  html.push('      borderWidth: 1');
  html.push('    }, {');
  html.push('      label: "Timeout Rate (%)",');
  html.push('      data: chartData.map(function(r) { return r.timeoutRate; }),');
  html.push('      backgroundColor: pointColors.map(function(c) { return c === passColor ? "rgba(10, 125, 55, 0.4)" : "rgba(176, 0, 32, 0.4)"; }),');
  html.push('      borderColor: pointColors,');
  html.push('      borderWidth: 1');
  html.push('    }]');
  html.push('  },');
  html.push('  options: {');
  html.push('    responsive: true,');
  html.push('    plugins: { legend: { position: "top" } },');
  html.push('    scales: { y: { beginAtZero: true, title: { display: true, text: "Rate (%)" } } }');
  html.push('  }');
  html.push('});');

  html.push('<\/script>');
  html.push('</body></html>');
  return html.join('');
}

export function buildHttpStepReport(data, stepPlan, options) {
  var metrics = data.metrics || {};

  var stepLatency = extractTrend(metrics, options.latencyMetric || 'steady_http_duration');
  var stepRequests = extractCount(metrics, options.requestsMetric || 'steady_http_requests');
  var stepErrors = extractCount(metrics, options.errorsMetric || 'steady_http_errors');
  var stepTimeouts = extractCount(metrics, options.timeoutsMetric || 'steady_http_timeout');
  var step4xx = extractCount(metrics, options.http4xxMetric || 'steady_http_4xx');
  var step5xx = extractCount(metrics, options.http5xxMetric || 'steady_http_5xx');
  var step429 = extractCount(metrics, options.http429Metric || 'steady_http_429');
  var stepIters = extractCount(metrics, options.itersMetric || 'steady_iters');

  var sloP95 = __ENV.SLO_P95_MS ? parseFloat(__ENV.SLO_P95_MS) : 500;
  var sloP99 = __ENV.SLO_P99_MS ? parseFloat(__ENV.SLO_P99_MS) : null;
  var sloErrorRate = __ENV.SLO_ERROR_RATE ? parseFloat(__ENV.SLO_ERROR_RATE) : 0.01;
  var sloTimeoutRate = __ENV.SLO_TIMEOUT_RATE ? parseFloat(__ENV.SLO_TIMEOUT_RATE) : 0;

  var rows = [];
  var lastPassing = null;

  for (var i = 0; i < stepPlan.steps.length; i++) {
    var step = stepPlan.steps[i];
    var key = step.step + '|' + step.vus;
    var latency = stepLatency[key] || {};
    var requests = stepRequests[key] || 0;
    var errors = stepErrors[key] || 0;
    var timeouts = stepTimeouts[key] || 0;
    var count4xx = step4xx[key] || 0;
    var count5xx = step5xx[key] || 0;
    var count429 = step429[key] || 0;
    var iters = stepIters[key] || 0;

    var stepSeconds = step.steadySeconds || stepPlan.steadySeconds || 0;
    var rps = stepSeconds > 0 ? requests / stepSeconds : 0;
    var itersPerSec = stepSeconds > 0 ? iters / stepSeconds : 0;
    var errorRate = requests > 0 ? errors / requests : 0;
    var timeoutRate = requests > 0 ? timeouts / requests : 0;
    var p95 = latency['p(95)'];
    var p99 = latency['p(99)'];

    var sloPass = true;
    if (requests === 0 || p95 === undefined) {
      sloPass = false;
    }
    if (p95 !== undefined && p95 > sloP95) {
      sloPass = false;
    }
    if (sloP99 !== null && p99 !== undefined && p99 > sloP99) {
      sloPass = false;
    }
    if (errorRate > sloErrorRate) {
      sloPass = false;
    }
    if (timeoutRate > sloTimeoutRate) {
      sloPass = false;
    }

    if (sloPass) {
      lastPassing = {
        step: step.step,
        vus: step.vus,
        rps: rps,
        itersPerSec: itersPerSec,
      };
    }

    // Collect failure reasons for this step
    var failReasons = [];
    if (requests === 0 || p95 === undefined) {
      failReasons.push('no data');
    }
    if (p95 !== undefined && p95 > sloP95) {
      failReasons.push('p95 latency exceeded (' + formatNumber(p95, 0) + 'ms > ' + sloP95 + 'ms)');
    }
    if (sloP99 !== null && p99 !== undefined && p99 > sloP99) {
      failReasons.push('p99 latency exceeded (' + formatNumber(p99, 0) + 'ms > ' + sloP99 + 'ms)');
    }
    if (errorRate > sloErrorRate) {
      failReasons.push('error rate exceeded (' + formatRate(errorRate) + ' > ' + formatRate(sloErrorRate) + ')');
    }
    if (timeoutRate > sloTimeoutRate) {
      failReasons.push('timeout rate exceeded (' + formatRate(timeoutRate) + ' > ' + formatRate(sloTimeoutRate) + ')');
    }

    rows.push({
      step: step.step,
      vus: step.vus,
      rps: rps,
      itersPerSec: itersPerSec,
      errorRate: errorRate,
      timeoutRate: timeoutRate,
      p90: latency['p(90)'],
      p95: p95,
      p99: p99,
      http4xx: count4xx,
      http5xx: count5xx,
      http429: count429,
      timeouts: timeouts,
      sloPass: sloPass,
      failReasons: failReasons,
    });
  }

  // Find bottleneck (first failing step)
  var bottleneck = null;
  for (var j = 0; j < rows.length; j++) {
    if (!rows[j].sloPass) {
      bottleneck = {
        step: rows[j].step,
        vus: rows[j].vus,
        rps: rows[j].rps,
        reasons: rows[j].failReasons,
      };
      break;
    }
  }

  var title = options.title || 'Step Report';

  return {
    html: renderStepHtml(rows, lastPassing, bottleneck, {
      sloP95: sloP95,
      sloP99: sloP99,
      sloErrorRate: sloErrorRate,
      sloTimeoutRate: sloTimeoutRate,
    }, title),
    json: {
      slo: {
        p95_ms: sloP95,
        p99_ms: sloP99,
        error_rate: sloErrorRate,
        timeout_rate: sloTimeoutRate,
      },
      capacity: lastPassing,
      bottleneck: bottleneck,
      rows: rows,
    },
  };
}
