# GoChat Load Testing Guide

GoChat 压测框架使用 [Grafana k6](https://k6.io/) 进行负载测试，支持 HTTP API 和 WebSocket 端点测试。

## 快速开始

```bash
# 运行完整系统测试 (默认配置)
make loadtest-full

# 运行阶梯式容量测试
make loadtest-capacity

# 查看 HTML 报告
open loadtest/reports/full-system.html
```

## 前置条件

- Docker 和 Docker Compose
- 至少 8GB 可用内存
- 端口可用: 7070, 7000, 9090, 3000

## 测试场景

### 1. 完整系统测试 (`full-system.js`)

同时测试所有 HTTP 端点和 WebSocket 连接:

```bash
# 固定用户量模式
make loadtest-full K6_VUS=200 K6_DURATION=10m

# 阶梯式负载模式
make loadtest-full K6_START_VUS=10 K6_END_VUS=200 K6_STEP_VUS=20
```

### 2. 容量基线测试 (`capacity-baseline.js`)

阶梯式递增负载，用于测试各服务的承载能力上限:

```bash
# 默认配置: 10 -> 20 -> ... -> 100 VUs
make loadtest-capacity

# 自定义阶梯: 10 -> 60 -> 110 -> ... -> 500 VUs
make loadtest-capacity K6_START_VUS=10 K6_END_VUS=500 K6_STEP_VUS=50

# 快速测试: 20 -> 40 -> 60 -> 80 -> 100 VUs, 每阶段30秒
make loadtest-capacity K6_START_VUS=20 K6_END_VUS=100 K6_STEP_VUS=20 K6_STEP_DURATION=30s
```

### 3. 单接口测试

```bash
# 登录接口
make loadtest-login K6_VUS=100 K6_DURATION=5m

# 注册接口
make loadtest-register K6_VUS=50 K6_DURATION=3m

# WebSocket 连接
make loadtest-websocket K6_VUS=100 K6_DURATION=5m

# 消息推送
make loadtest-push K6_VUS=50 K6_DURATION=3m

# 房间广播
make loadtest-pushroom K6_VUS=50 K6_DURATION=3m
```

### 4. 烟雾测试

快速验证系统是否正常工作:

```bash
make loadtest-smoke  # 5 VUs, 30秒
```

## 配置参数

所有参数均可在运行时通过命令行指定:

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `K6_VUS` | 固定虚拟用户数 | - |
| `K6_DURATION` | 测试持续时间 | 5m |
| `K6_START_VUS` | 阶梯起始用户数 | 10 |
| `K6_END_VUS` | 阶梯结束用户数 | 100 |
| `K6_STEP_VUS` | 每阶梯增长用户数 | 10 |
| `K6_STEP_DURATION` | 每阶梯持续时间 | 1m |
| `K6_RAMP_DURATION` | 阶梯间爬升时间 | 30s |

### 模式选择

- **固定 VUs 模式**: 设置 `K6_VUS` 参数时启用
- **阶梯模式**: 未设置 `K6_VUS` 时启用，使用 `K6_START_VUS`, `K6_END_VUS`, `K6_STEP_VUS` 参数

### 示例

```bash
# 固定 200 用户，持续 10 分钟
make loadtest-full K6_VUS=200 K6_DURATION=10m

# 从 50 递增到 1000，每次增加 100，每阶段 2 分钟
make loadtest-capacity K6_START_VUS=50 K6_END_VUS=1000 K6_STEP_VUS=100 K6_STEP_DURATION=2m

# 快速阶梯测试，每阶段 30 秒，爬升 10 秒
make loadtest-capacity K6_STEP_DURATION=30s K6_RAMP_DURATION=10s
```

## 测试架构

```
┌─────────────┐     ┌─────────────┐
│   k6        │────▶│   API       │──┬──▶ Logic ──▶ Redis
│   Container │     │   :7070     │  │
└─────────────┘     └─────────────┘  └──▶ Task
       │
       │            ┌─────────────┐
       └───────────▶│ Connect-WS  │──────▶ Logic
         WebSocket  │   :7000     │
                    └─────────────┘
```

## 资源限制

压测环境中各服务的资源限制 (用于建立基线):

| 服务 | CPU 限制 | 内存限制 |
|------|----------|----------|
| API | 1.0 | 512M |
| Logic | 1.0 | 512M |
| Connect-WS | 1.0 | 512M |
| Connect-TCP | 1.0 | 512M |
| Task | 1.0 | 512M |
| Site | 0.5 | 256M |
| etcd | 0.5 | 256M |
| redis | 0.5 | 256M |

## 测试指标

### 默认阈值

| 指标 | 阈值 | 说明 |
|------|------|------|
| `http_req_duration p(95)` | < 500ms | 95% 请求延迟 |
| `http_req_duration p(99)` | < 1000ms | 99% 请求延迟 |
| `http_req_failed` | < 5% | 错误率 |
| `ws_connect_duration p(95)` | < 2000ms | WebSocket 连接延迟 |

### 自定义指标

每个测试脚本都定义了特定的指标:

- `login_success_rate` - 登录成功率
- `register_success_rate` - 注册成功率
- `push_success_rate` - 消息推送成功率
- `ws_connect_success` - WebSocket 连接成功率

## 报告

测试完成后，HTML 报告自动生成在 `loadtest/reports/` 目录:

```bash
# 报告文件
loadtest/reports/full-system.html
loadtest/reports/capacity-baseline.html
loadtest/reports/user-login.html
loadtest/reports/websocket.html
# ...
```

### 报告内容

- 执行摘要 (通过/失败状态)
- 各阶段指标明细
- 响应时间分布图
- 错误率分析
- 吞吐量趋势

## 实时监控

启动 Grafana 进行实时监控:

```bash
make loadtest-grafana
# 访问 http://localhost:3000 (admin/admin)
```

## 目录结构

```
loadtest/
├── scripts/
│   ├── lib/
│   │   ├── config.js          # 测试配置
│   │   ├── auth.js            # 认证工具
│   │   └── helpers.js         # 通用工具
│   ├── scenarios/
│   │   ├── user-login.js      # 登录测试
│   │   ├── user-register.js   # 注册测试
│   │   ├── push-push.js       # 消息推送测试
│   │   ├── push-room.js       # 房间广播测试
│   │   └── websocket.js       # WebSocket 测试
│   ├── full-system.js         # 完整系统测试
│   └── capacity-baseline.js   # 容量基线测试
├── reports/                   # 测试报告
├── docker-compose.loadtest.yml
└── README.md
```

## 故障排查

### 服务启动失败

```bash
# 检查服务状态
make compose-ps

# 查看日志
make compose-logs
```

### 高错误率

1. 检查资源使用: `docker stats`
2. 查看 API 日志寻找错误信息
3. 降低 VU 数量重试

### k6 容器无法连接服务

确保服务在 `gochat-network` 网络中:

```bash
docker network inspect gochat-scale_gochat-network
```

## 清理

```bash
# 停止服务
make loadtest-stop

# 清理报告和容器
make loadtest-clean
```

## 扩展测试

### 添加自定义测试脚本

1. 在 `loadtest/scripts/scenarios/` 创建新脚本
2. 使用 `lib/config.js` 和 `lib/auth.js` 工具
3. 运行: `make loadtest-custom LOADTEST_SCRIPT=scenarios/your-script.js`

### 示例自定义脚本

```javascript
import http from 'k6/http';
import { check, sleep } from 'k6';
import { buildStages, getFixedConfig, baseUrl } from '../lib/config.js';

const useFixedVus = __ENV.K6_VUS !== undefined;

export const options = {
  scenarios: {
    custom_test: useFixedVus
      ? { executor: 'constant-vus', vus: getFixedConfig().vus, duration: getFixedConfig().duration }
      : { executor: 'ramping-vus', startVUs: 0, stages: buildStages() },
  },
};

export default function () {
  const res = http.get(`${baseUrl}/your-endpoint`);
  check(res, { 'status is 200': (r) => r.status === 200 });
  sleep(1);
}
```
