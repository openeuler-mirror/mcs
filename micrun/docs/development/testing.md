# MicRun 测试指南

> **本文档说明**：本指南介绍 MicRun 的测试方法、测试用例和验证步骤，帮助开发者和用户验证 MicRun 的功能正确性。

## 目录

- [测试概述](#测试概述)
- [环境准备](#环境准备)
- [单元测试](#单元测试)
- [集成测试](#集成测试)
- [端到端测试](#端到端测试)
- [性能测试](#性能测试)
- [故障注入测试](#故障注入测试)
- [测试最佳实践](#测试最佳实践)

---

## 测试概述

### 测试金字塔

```
                    ┌─────────────────┐
                    │  E2E Tests      │  少量，端到端验证
                    │  (端到端测试)    │
                    ├─────────────────┤
                    │  Integration    │  中等，模块协作验证
                    │  Tests          │
                    ├─────────────────┤
                    │  Unit Tests     │  大量，快速反馈
                    │  (单元测试)      │
                    └─────────────────┘
```

### 测试分类

| 测试类型 | 目的 | 工具 | 执行频率 |
|---------|------|------|---------|
| **单元测试** | 测试单个函数/方法的正确性 | `go test` | 每次代码变更 |
| **集成测试** | 测试模块间的交互 | `bash` + `ctr` | 每次提交 |
| **端到端测试** | 测试完整用户场景 | `kubectl` + `k3s` | 发布前 |
| **性能测试** | 测试资源使用和响应时间 | `perf`, `top` | 定期 |
| **故障测试** | 测试异常情况下的行为 | 手动模拟 | 定期 |

---

## 环境准备

### 测试环境要求

| 组件 | 版本要求 | 说明 |
|------|---------|------|
| 操作系统 | openEuler Embedded | 包含 Xen, micad |
| Go | 1.21+ | 运行单元测试 |
| containerd | 1.7.27+ | 容器引擎 |
| K3s | 1.28+ | E2E 测试（可选） |
| QEMU | 6.0+ | 虚拟化测试环境 |

### 快速设置测试环境

```bash
# 1. 进入项目目录
cd /path/to/mcs/micrun

# 2. 编译 MicRun
make build

# 3. 安装到测试环境
sudo cp build/micrun /usr/local/bin/
sudo chmod +x /usr/local/bin/micrun

# 4. 准备测试镜像（如果还没有）
cd tests/fixtures
./build-test-image.sh

# 5. 导入测试镜像
ctr image import test-image.tar
```

### 测试镜像

MicRun 提供测试镜像用于自动化测试：

| 镜像名称 | 说明 | 路径 |
|---------|------|------|
| `localhost:5000/mica-test:latest` | 通用测试镜像 | `tests/fixtures/` |
| `localhost:5000/mica-uniproton-app:xen-0.1` | UniProton 测试镜像 | 需自行构建 |

---

## 单元测试

### 运行所有单元测试

```bash
# 在项目根目录执行
cd /path/to/mcs/micrun

# 运行所有单元测试
go test ./...

# 运行并显示覆盖率
go test -cover ./...

# 运行并生成覆盖率报告
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### 运行特定包的测试

```bash
# 测试 IO 包
go test -v ./pkg/io/...

# 测试 Shim 包
go test -v ./pkg/shim/...

# 测试特定函数
go test -v ./pkg/io/... -run TestNewSession
```

### 单元测试示例

```go
// pkg/io/copier_test.go
func TestCopierStart(t *testing.T) {
    // 创建测试配置
    config := micrunio.DefaultConfig()
    config.ContainerID = "test-container"
    config.Terminal = true

    // 创建 copier
    copier := micrunio.NewCopier(config)

    // 测试启动
    err := copier.Start()
    if err != nil {
        t.Fatalf("Failed to start copier: %v", err)
    }

    // 清理
    copier.Stop()
}
```

### 常用测试命令

```bash
# 详细输出
go test -v ./pkg/io/...

# 竞态检测
go test -race ./pkg/io/...

# 基准测试
go test -bench=. ./pkg/io/...

# 性能分析
go test -cpuprofile=cpu.prof ./pkg/io/...
go tool pprof cpu.prof
```

---

## 集成测试

### 集成测试目录结构

```
tests/
├── io/                     # IO 测试
│   ├── test_new_io_start_detach.sh
│   ├── test_new_io_attach_detach.sh
│   └── test_ctrl_q_ctrl_q_detach.sh
├── lifecycle/              # 生命周期测试
│   ├── test_1_1_1_lifecycle.sh
│   └── test_state_api.sh
└── fixtures/               # 测试固件
    └── test-images/
```

### 运行集成测试

```bash
# 进入测试目录
cd /path/to/mcs/micrun/tests

# 运行所有 IO 测试
cd io
./test_new_io_start_detach.sh
./test_new_io_attach_detach.sh

# 运行生命周期测试
cd ../lifecycle
./test_1_1_1_lifecycle.sh
```

### IO 测试详解

#### 测试 1: 新 IO 系统启动和分离

**文件**: `tests/io/test_new_io_start_detach.sh`

**测试目的**：验证 IO 系统的启动、用户交互和分离功能

**测试步骤**：

```bash
#!/bin/bash
set -e

CONTAINER_NAME="test-io-start-detach"
IMAGE="localhost:5000/mica-uniproton-app:xen-0.1"

echo "=== Step 1: 创建容器 ==="
ctr container create \
  --runtime io.containerd.mica.v2 \
  -t \
  $IMAGE $CONTAINER_NAME

echo "=== Step 2: 启动容器（后台）==="
ctr task start -d $CONTAINER_NAME

echo "=== Step 3: 等待 10 秒 ==="
sleep 10

echo "=== Step 4: Attach 到容器 ==="
expect -c "
set timeout 30
spawn ctr task attach $CONTAINER_NAME
expect "hello"
send "help\r"
expect "Available commands"
send "\x11\x11"  # Ctrl+Q Ctrl+Q detach
expect eof
"

echo "=== Step 5: 验证 shim 仍在运行 ==="
ps aux | grep containerd-shim-mica-v2 | grep $CONTAINER_NAME

echo "=== Step 6: 清理 ==="
ctr task delete $CONTAINER_NAME
ctr container delete $CONTAINER_NAME

echo "=== 测试通过 ==="
```

**预期结果**：
- ✅ 容器成功创建和启动
- ✅ attach 后能看到输出（如 "hello"）
- ✅ Ctrl+Q Ctrl+Q 成功分离
- ✅ shim 进程持续运行
- ✅ 清理成功

#### 测试 2: 新 IO 系统 Attach 和 Detach

**文件**: `tests/io/test_new_io_attach_detach.sh`

**测试目的**：验证多次 attach/detach 的稳定性

**关键测试点**：
- 第一次 attach 后 detach，容器保持运行
- 第二次 attach 能正常连接
- 多次 detach 不会导致资源泄漏

#### 测试 3: Ctrl+Q Ctrl+Q Detach

**文件**: `tests/io/test_ctrl_q_ctrl_q_detach.sh`

**测试目的**：验证自定义的 detach 序列

**测试场景**：

```bash
# 用户输入 help 命令
# 预期：看到帮助信息
help
Available commands:
  - help
  - version
  - exit

# 用户按 Ctrl+Q Ctrl+Q
# 预期：成功分离，返回 shell
# Shim 继续运行，容器状态为 STOPPED
```

### 生命周期测试详解

#### 测试 1: 1:1:1 生命周期验证

**文件**: `tests/lifecycle/test_1_1_1_lifecycle.sh`

**测试目的**：验证 RTOS:Sandbox:Shim 的 1:1:1 分离模型

**测试步骤**：

```bash
#!/bin/bash
set -e

CONTAINER_NAME="test-lifecycle"
IMAGE="localhost:5000/mica-uniproton-app:xen-0.1"

echo "=== 创建容器 ==="
ctr container create --runtime io.containerd.mica.v2 -t $IMAGE $CONTAINER_NAME

echo "=== 启动容器（后台模式）==="
ctr task start -d $CONTAINER_NAME

# 获取 shim PID
SHIM_PID=$(ps aux | grep containerd-shim-mica-v2 | grep $CONTAINER_NAME | awk '{print $2}')
echo "Shim PID: $SHIM_PID"

echo "=== 等待 35 秒（auto_close 超时）==="
sleep 35

echo "=== 验证容器已停止 ==="
ctr task ls | grep $CONTAINER_NAME | grep STOPPED

echo "=== 验证 shim 仍在运行 ==="
ps -p $SHIM_PID > /dev/null
echo "Shim (PID $SHIM_PID) is still running ✓"

echo "=== 验证 State API ==="
ctr task stats $CONTAINER_NAME

echo "=== 清理 ==="
ctr task delete $CONTAINER_NAME
ctr container delete $CONTAINER_NAME

echo "=== 验证 shim 已退出 ==="
sleep 2
! ps -p $SHIM_PID > /dev/null
echo "Shim has exited ✓"

echo "=== 1:1:1 生命周期测试通过 ==="
```

**预期结果**：
- ✅ 容器启动后运行 35 秒停止
- ✅ 容器停止后 shim 继续运行
- ✅ State API 能正确查询状态
- ✅ Delete 后 shim 正确退出

#### 测试 2: State API 无死锁

**文件**: `tests/lifecycle/test_state_api.sh`

**测试目的**：验证 State API 不会死锁

**关键测试点**：
- 容器运行时调用 State，立即返回
- 容器停止后调用 State，返回 STOPPED
- 并发调用 State，不出现死锁

### 运行所有集成测试

```bash
# 创建测试脚本包装器
cat > run-all-integration-tests.sh <<'EOF'
#!/bin/bash
set -e

BASE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "========================================="
echo "Running MicRun Integration Tests"
echo "========================================="

# IO 测试
echo ""
echo "[1/3] IO Tests"
cd $BASE_DIR/tests/io
./test_new_io_start_detach.sh
./test_new_io_attach_detach.sh
./test_ctrl_q_ctrl_q_detach.sh

# 生命周期测试
echo ""
echo "[2/2] Lifecycle Tests"
cd $BASE_DIR/tests/lifecycle
./test_1_1_1_lifecycle.sh
./test_state_api.sh

# 注意：Ctrl+C 测试已废弃，micrun 不处理 SIGINT 信号
# 推荐使用 "exit" 命令或 `ctr task kill` 终止容器

echo ""
echo "========================================="
echo "All Integration Tests Passed! ✓"
echo "========================================="
EOF

chmod +x run-all-integration-tests.sh
./run-all-integration-tests.sh
```

---

## 端到端测试

### E2E 测试概述

端到端测试模拟真实用户场景，从创建容器到删除容器的完整流程。

### E2E 测试场景

#### 场景 1: 云边协同部署

**文件**: `tests/e2e/test_cloud_edge_deployment.sh`

**测试目的**：验证通过 Kubernetes 部署 RTOS 容器

**前置条件**：
- 云侧：K3s Server 运行中
- 边侧：K3s Agent + MicRun 运行中
- 网络：云边节点互通

**测试步骤**：

```bash
#!/bin/bash
set -e

# 1. 在云侧创建 RuntimeClass
kubectl apply -f - <<EOF
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: micrun
handler: micrun
EOF

# 2. 创建测试 Pod
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: e2e-rtos-test
spec:
  runtimeClassName: micrun
  nodeName: edge  # 指定边侧节点
  containers:
  - name: rtos
    image: localhost:5000/mica-uniproton-app:xen-0.1
    tty: true
    stdin: true
EOF

# 3. 等待 Pod 启动
echo "Waiting for pod to be ready..."
kubectl wait --for=condition=ready pod/e2e-rtos-test --timeout=60s

# 4. 验证 Pod 状态
kubectl get pods e2e-rtos-test
kubectl describe pod e2e-rtos-test

# 5. 在边侧验证容器
ssh root@edge "ctr task ls | grep e2e-rtos-test"

# 6. 查看日志
kubectl logs e2e-rtos-test --tail=50

# 7. 清理
kubectl delete pod e2e-rtos-test
kubectl wait --for=delete pod/e2e-rtos-test --timeout=30s

echo "E2E cloud-edge deployment test passed! ✓"
```

#### 场景 2: 高可用 Deployment

**测试目的**：验证 Deployment 管理多个 RTOS 实例

**测试步骤**：

```bash
# 1. 创建 Deployment
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rtos-ha-test
spec:
  replicas: 3
  selector:
    matchLabels:
      app: rtos-test
  template:
    metadata:
      labels:
        app: rtos-test
    spec:
      runtimeClassName: micrun
      containers:
      - name: rtos
        image: localhost:5000/mica-uniproton-app:xen-0.1
        tty: true
        stdin: true
EOF

# 2. 等待所有 Pod 就绪
kubectl rollout status deployment/rtos-ha-test

# 3. 验证副本数
[ $(kubectl get pods -l app=rtos-test --no-headers | wc -l) -eq 3 ]

# 4. 模拟 Pod 故障
kubectl delete pod -l app=rtos-test --random-

# 5. 验证自动恢复
sleep 10
[ $(kubectl get pods -l app=rtos-test --no-headers | wc -l) -eq 3 ]

# 6. 清理
kubectl delete deployment rtos-ha-test

echo "HA deployment test passed! ✓"
```

### 运行 E2E 测试

```bash
# 设置云边环境（假设已配置）
export CLOUD_NODE="root@192.168.1.100"
export EDGE_NODE="root@192.168.1.101"

# 运行 E2E 测试
cd /path/to/mcs/micrun/tests/e2e
scp test_cloud_edge_deployment.sh $CLOUD_NODE:/tmp/
ssh $CLOUD_NODE "bash /tmp/test_cloud_edge_deployment.sh"
```

---

## 性能测试

### 性能指标

| 指标 | 目标值 | 测量方法 |
|------|--------|---------|
| 容器启动时间 | < 6 秒 | `time ctr task start` |
| 内存占用 (shim) | < 20 MB | `ps aux \| grep shim` |
| CPU 使用率 (空闲) | < 1% | `top` |
| IO 吞吐量 | > 1 MB/s | `dd if=/dev/zero of=/dev/ttyRPMSG0` |
| 响应延迟 | < 100 ms | `echo test` → 看到输出 |

### 性能测试脚本

**文件**: `tests/performance/test_perf.sh`

```bash
#!/bin/bash

CONTAINER_NAME="perf-test"
IMAGE="localhost:5000/mica-uniproton-app:xen-0.1"

echo "=== 性能测试: 容器启动时间 ==="
time ctr task start -d $CONTAINER_NAME

echo "=== 性能测试: 内存占用 ==="
sleep 5
ps aux | grep containerd-shim-mica-v2 | grep $CONTAINER_NAME | awk '{print "Shim Memory: " $6 " KB"}'

echo "=== 性能测试: CPU 使用率 ==="
top -b -n 1 | grep containerd-shim-mica-v2

echo "=== 性能测试: IO 吞吐量 ==="
# 需要在容器内部测试
ctr task exec --exec-id test $CONTAINER_NAME dd if=/dev/zero of=/dev/null bs=1M count=100 2>&1 | grep copied

echo "=== 清理 ==="
ctr task delete $CONTAINER_NAME
ctr container delete $CONTAINER_NAME
```

### 压力测试

**测试目的**：验证系统在高负载下的稳定性

```bash
# 1. 启动多个容器
for i in {1..10}; do
  ctr container create --runtime io.containerd.mica.v2 -t \
    $IMAGE "stress-test-$i"
  ctr task start -d "stress-test-$i"
done

# 2. 持续监控
watch -n 1 'echo "=== Container Count ==="; ctr task ls | wc -l; echo "=== Memory ==="; free -h'

# 3. 清理
for i in {1..10}; do
  ctr task delete "stress-test-$i"
  ctr container delete "stress-test-$i"
done
```

---

## 故障注入测试

### 测试目的

验证 MicRun 在异常情况下的健壮性

### 故障场景

#### 场景 1: Shim 进程崩溃

**测试步骤**：

```bash
# 1. 启动容器
ctr task start -d test-crash

# 2. 杀掉 shim 进程
SHIM_PID=$(ps aux | grep containerd-shim-mica-v2 | grep test-crash | awk '{print $2}')
kill -9 $SHIM_PID

# 3. 验证 containerd 检测到崩溃
sleep 5
ctr task ls | grep test-crash | grep -i "unknown\|stopped"

# 4. 清理
ctr task delete -f test-crash
ctr container delete test-crash
```

**预期结果**：
- containerd 检测到 shim 崩溃
- 任务状态显示为 UNKNOWN 或 STOPPED
- 可以强制删除任务

#### 场景 2: 网络中断

**测试步骤**：

```bash
# 1. 启动容器
ctr task start -d test-network

# 2. 模拟网络中断（如果使用远程存储）
# iptables -A INPUT -p tcp --dport 6443 -j DROP

# 3. 尝试查询状态
ctr task stats test-network

# 4. 恢复网络
# iptables -D INPUT -p tcp --dport 6443 -j DROP

# 5. 清理
ctr task delete test-network
```

#### 场景 3: 资源耗尽

**测试步骤**：

```bash
# 1. 限制内存
ulimit -v 10240  # 限制 10 MB

# 2. 尝试启动容器
ctr task start -d test-oom

# 3. 检查是否正确处理
dmesg | grep -i "out of memory"

# 4. 清理
ulimit -v unlimited
```

---

## 测试最佳实践

### 1. 测试隔离

每个测试应该独立运行，不依赖其他测试的状态：

```bash
# 好的做法
test_case_1() {
  CONTAINER="test-unique-$(date +%s)"
  # 测试逻辑
  cleanup $CONTAINER
}

# 不好的做法
CONTAINER="test-shared"
test_case_1() { ... }
test_case_2() { ... }  # 依赖 test_case_1 的状态
```

### 2. 清理资源

测试失败时也要清理资源：

```bash
#!/bin/bash
set -e

trap cleanup EXIT

cleanup() {
  echo "Cleaning up..."
  ctr task delete -f test-container 2>/dev/null || true
  ctr container delete test-container 2>/dev/null || true
}

# 测试逻辑
```

### 3. 使用 expect 自动化交互

对于需要交互的测试，使用 `expect`：

```tcl
expect {
  "hello" {
    send "help\r"
    expect "Available commands"
    send "\x11\x11"  # detach
  }
  timeout {
    puts "Test timed out"
    exit 1
  }
}
```

### 4. 记录详细日志

```bash
#!/bin/bash

LOG_FILE="/tmp/test-$(date +%Y%m%d-%H%M%S).log"

exec > >(tee -a $LOG_FILE)
exec 2>&1

echo "=== Test started at $(date) ==="

# 测试逻辑

echo "=== Test finished at $(date) ==="
echo "Log saved to: $LOG_FILE"
```

### 5. 断言和验证

```bash
# 定义断言函数
assert_equals() {
  local expected="$1"
  local actual="$2"
  local message="${3:-Assertion failed}"

  if [ "$expected" != "$actual" ]; then
    echo "FAILED: $message"
    echo "  Expected: $expected"
    echo "  Actual: $actual"
    exit 1
  fi
}

# 使用
CONTAINER_STATUS=$(ctr task ls | grep test-container | awk '{print $2}')
assert_equals "RUNNING" "$CONTAINER_STATUS" "Container should be running"
```

### 6. 并行测试

对于独立的测试，可以并行执行：

```bash
# GNU parallel
ls tests/*.sh | parallel -j 4 {}

# 或使用后台任务
for test in tests/*.sh; do
  $test &
done
wait
```

---

## CI/CD 集成

### GitHub Actions 示例

```yaml
name: MicRun Tests

on:
  push:
    branches: [ master, develop ]
  pull_request:
    branches: [ master ]

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v3
    - name: Set up Go
      uses: actions/setup-go@v3
      with:
        go-version: 1.21
    - name: Run unit tests
      run: |
        go test -v -race -coverprofile=coverage.out ./...
        go tool cover -html=coverage.out -o coverage.html
    - name: Upload coverage
      uses: actions/upload-artifact@v3
      with:
        name: coverage-report
        path: coverage.html

  integration-tests:
    runs-on: ubuntu-latest
    needs: unit-tests
    steps:
    - uses: actions/checkout@v3
    - name: Setup test environment
      run: |
        ./scripts/setup-test-env.sh
    - name: Run integration tests
      run: |
        cd tests
        ./run-all-integration-tests.sh
```

---

## 附录

### A. 测试检查清单

在提交代码前，确保通过以下检查：

- [ ] 所有单元测试通过 (`go test ./...`)
- [ ] 代码覆盖率 > 80%
- [ ] 无竞态条件警告 (`go test -race ./...`)
- [ ] 集成测试全部通过
- [ ] 手动测试核心场景
- [ ] 文档更新完整

### B. 常见问题

**Q: 测试失败后如何调试？**
A:
1. 查看测试日志
2. 在本地手动复现
3. 使用 `go test -v` 查看详细输出
4. 添加 `set -x` 开启 bash 调试

**Q: 如何在 CI 中运行需要权限的测试？**
A: 在 CI 配置中使用 sudo 或配置容器化环境。

**Q: 测试镜像很大，如何加速测试？**
A:
1. 使用更小的测试镜像（如 Alpine-based）
2. 预先拉取镜像
3. 使用镜像缓存

### C. 参考资料

- [Go Testing 官方文档](https://golang.org/pkg/testing/)
- [Bash Testing Best Practices](https://github.com/sstephenson/bats)
- [Kubernetes Testing](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-testing/testing.md)

---

**下一步**：
- 运行完整的测试套件
- 为新功能添加测试用例
- 持续改进测试覆盖率

**问题反馈**：
- 提交测试相关的 Issue: [https://atomgit.com/openeuler/mcs/issues](https://atomgit.com/openeuler/mcs/issues)
