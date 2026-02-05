# MicRun 测试框架指南

> **本文档说明**：介绍 MicRun 测试框架的结构、使用方法和扩展指南，方便添加新的测试类别。

## 目录

- [测试框架概述](#测试框架概述)
- [测试目录结构](#测试目录结构)
- [环境配置](#环境配置)
- [测试类别](#测试类别)
- [添加新测试](#添加新测试)
- [测试执行](#测试执行)
- [最佳实践](#最佳实践)

---

## 测试框架概述

### 设计原则

1. **模块化**: 每个测试类别独立管理
2. **可扩展**: 新增测试类别只需添加目录和配置
3. **一键执行**: 支持按类别或全部执行测试
4. **统一输出**: 测试结果格式统一，便于 CI/CD 集成

### 测试金字塔

```
                    ┌─────────────────┐
                    │  E2E Tests      │  云化场景、端到端
                    │  k3s/云边协同    │
                    ├─────────────────┤
                    │  Integration    │  IO、生命周期、性能
                    │  Tests          │
                    ├─────────────────┤
                    │  Unit Tests     │  Go 单元测试
                    │  (单元测试)      │
                    └─────────────────┘
```

---

## 测试目录结构

```
micrun/
├── docs/
│   └── test-framework.md          # 本文档
├── tests/
│   ├── lib/                       # 测试框架库
│   │   ├── test_runner.sh         # 测试运行器
│   │   ├── test_utils.sh          # 测试工具函数
│   │   └── test_reporter.sh       # 测试报告生成器
│   ├── io/                        # IO 测试
│   │   ├── run_io_tests.sh        # IO 测试入口
│   │   ├── test_*.exp             # expect 脚本
│   │   └── README.md
│   ├── k3s/                       # K3s 云化测试（新增）
│   │   ├── run_k3s_tests.sh       # K3s 测试入口
│   │   ├── test_pod_lifecycle.sh  # Pod 生命周期测试
│   │   ├── test_deployment.sh     # Deployment 测试
│   │   └── README.md
│   ├── lifecycle/                 # 生命周期测试
│   ├── performance/               # 性能测试
│   └── fixtures/                  # 测试固件
│       └── images/
└── run_all_tests.sh               # 全局测试入口
```

---

## 环境配置

### 测试环境配置文件

**文件**: `tests/test-env.sh`

```bash
#!/bin/bash
# MicRun 测试环境配置
# 使用方式: source tests/test-env.sh

# 远程主机配置（用于分布式测试）
export TEST_REMOTE_HOST="${TEST_REMOTE_HOST:-root@192.168.7.2}"

# 测试镜像配置
export TEST_IMAGE="${TEST_IMAGE:-localhost:5000/mica-uniproton-app:xen-0.1}"

# K3s 配置
export K3S_MASTER_URL="${K3S_MASTER_URL:-https://192.168.1.100:6443}"
export K3S_MASTER_NODE="${K3S_MASTER_NODE:-root@192.168.1.100}"
export K3S_WORKER_NODES="${K3S_WORKER_NODES:-root@192.168.1.101}"

# 测试超时配置
export TEST_TIMEOUT_DEFAULT="${TEST_TIMEOUT_DEFAULT:-60}"
export TEST_TIMEOUT_POD="${TEST_TIMEOUT_POD:-120}"

# 测试日志目录
export TEST_LOG_DIR="${TEST_LOG_DIR:-/var/log/micrun-tests}"

# 创建日志目录
mkdir -p "$TEST_LOG_DIR"

echo "MicRun 测试环境:"
echo "  REMOTE_HOST=$TEST_REMOTE_HOST"
echo "  TEST_IMAGE=$TEST_IMAGE"
echo "  K3S_MASTER=$K3S_MASTER_NODE"
echo "  LOG_DIR=$TEST_LOG_DIR"
```

### 镜像准备

```bash
# 方式1: 从 tarball 导入
scp mica-uniproton-app-xen-0.1.tar.gz root@192.168.7.2:/tmp/
ssh root@192.168.7.2 "ctr image import /tmp/mica-uniproton-app-xen-0.1.tar.gz"

# 方式2: 从私有仓库拉取（如有）
ssh root@192.168.7.2 "ctr image pull localhost:5000/mica-uniproton-app:xen-0.1"
```

---

## 测试类别

### 1. IO 测试 (tests/io/)

**测试内容**: 标准输入输出、TTY、回声抑制、attach/detach

| ID | 测试名称 | 说明 |
|----|----------|------|
| IO-001 | ctr 后台模式 attach | 验证后台容器 attach 功能 |
| IO-002 | ctr 前台模式 | 验证前台模式启动 |
| IO-003 | 非 TTY 模式 | 验证非交互式输入 |
| IO-004 | 多命令执行 | 验证连续命令处理 |
| IO-005 | 退出命令检测 | 验证 exit 命令处理 |
| IO-006 | 日志清洁度 | 验证无日志垃圾 |
| IO-007 | TTY 回声抑制 | 验证无双重回显 |

**执行方式**:
```bash
cd tests/io
./run_io_tests.sh
```

### 2. K3s 云化测试 (tests/k3s/)

**测试内容**: Kubernetes 集成、Pod 生命周期、Deployment

| ID | 测试名称 | 说明 |
|----|----------|------|
| K3S-001 | RuntimeClass 创建 | 验证 RuntimeClass 配置 |
| K3S-002 | Pod 启动/停止 | 验证 Pod 生命周期 |
| K3S-003 | Deployment 扩缩容 | 验证副本管理 |
| K3S-004 | Pod 日志获取 | 验证日志输出 |
| K3S-005 | 资源限制 | 验证 CPU/Memory 限制 |
| K3S-006 | 云边协同 | 验证多节点部署 |
| K3S-007 | 故障恢复 | 验证 Pod 自动重建 |

**执行方式**:
```bash
cd tests/k3s
./run_k3s_tests.sh
```

### 3. 生命周期测试 (tests/lifecycle/)

**测试内容**: 容器创建、启动、停止、删除

| ID | 测试名称 | 说明 |
|----|----------|------|
| LIFE-001 | 1:1:1 模型验证 | 验证 shim:sandbox:container 关系 |
| LIFE-002 | State API | 验证状态查询 |
| LIFE-003 | 优雅退出 | 验证 SIGTERM 处理 |
| LIFE-004 | 强制删除 | 验证 SIGKILL 处理 |

### 4. 性能测试 (tests/performance/)

**测试内容**: 启动时间、资源占用、吞吐量

| ID | 测试名称 | 说明 |
|----|----------|------|
| PERF-001 | 容器启动时间 | 目标 < 6 秒 |
| PERF-002 | 内存占用 | 目标 < 20 MB |
| PERF-003 | CPU 使用率 | 目标 < 1% (空闲) |
| PERF-004 | 并发启动 | 验证多容器场景 |

---

## 添加新测试

### 步骤 1: 创建测试目录

```bash
cd tests
mkdir <new-test-category>
cd <new-test-category>
```

### 步骤 2: 创建测试入口脚本

**文件**: `tests/<category>/run_<category>_tests.sh`

```bash
#!/bin/bash
# <Category> 测试套件入口
# 使用: ./run_<category>_tests.sh [test_id]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/../lib/test_runner.sh"
source "${SCRIPT_DIR}/../lib/test_utils.sh"

# 测试配置
CATEGORY="<category>"
TEST_PREFIX="<PREFIX>"  # 如 IO, K3S, LIFE 等

# 测试用例定义
declare -A TEST_CASES=(
    ["${TEST_PREFIX}-001"]="test_case_1|Test Case 1 Description"
    ["${TEST_PREFIX}-002"]="test_case_2|Test Case 2 Description"
    # 添加更多测试用例...
)

# ============================================
# 测试用例实现
# ============================================

test_case_1() {
    log_test "${TEST_PREFIX}-001" "Test Case 1 Description"
    local start=$(date +%s)

    # 测试逻辑
    ...

    local end=$(date +%s)
    local time=$((end - start))

    # 验证结果
    if [条件]; then
        record_result "${TEST_PREFIX}-001" "PASS" "Details" "$time"
        echo -e "$PASS"
    else
        record_result "${TEST_PREFIX}-001" "FAIL" "Details" "$time"
        echo -e "$FAIL"
    fi
}

test_case_2() {
    # 类似实现...
    :
}

# ============================================
# 主函数
# ============================================

main() {
    local test_id="${1:-}"

    print_header "$CATEGORY Test Suite"

    if [ -n "$test_id" ]; then
        # 运行单个测试
        run_single_test "$test_id"
    else
        # 运行所有测试
        run_all_tests

        # 打印结果
        print_results

        # 清理
        cleanup_all
    fi
}

# 导出测试用例（供框架调用）
export_test_cases TEST_CASES

# 运行主函数
main "$@"
```

### 步骤 3: 创建 README.md

**文件**: `tests/<category>/README.md`

```markdown
# <Category> 测试

## 测试概述

本目录包含 <Category> 相关的集成测试。

## 环境要求

- [ ] 要求 1
- [ ] 要求 2

## 测试用例

| ID | 测试名称 | 说明 |
|----|----------|------|
| XXX-001 | 测试1 | 说明 |

## 执行方式

```bash
# 运行所有测试
./run_<category>_tests.sh

# 运行单个测试
./run_<category>_tests.sh XXX-001
```

## 参考资料

- 相关文档链接
```

### 步骤 4: 注册到全局测试入口

更新 `tests/run_all_tests.sh`:

```bash
#!/bin/bash
# MicRun 测试套件 - 全局入口
# 使用: ./run_all_tests.sh [category] [test_id]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/test-env.sh"

# 测试类别注册
declare -A TEST_CATEGORIES=(
    ["io"]="IO 测试"
    ["k3s"]="K3s 云化测试"
    ["lifecycle"]="生命周期测试"
    ["performance"]="性能测试"
    ["<category>"]="<Category> 测试"  # 添加新类别
)

# ... 其余实现
```

---

## 测试执行

### 执行单个测试类别

```bash
cd tests/io
./run_io_tests.sh
```

### 执行所有测试

```bash
cd tests
./run_all_tests.sh
```

### 执行特定测试用例

```bash
cd tests/io
./run_io_tests.sh IO-001
```

### CI/CD 集成

```yaml
# .github/workflows/test.yml
name: MicRun Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: [self-hosted, edge]
    steps:
      - name: Checkout
        uses: actions/checkout@v3

      - name: Setup
        run: |
          source tests/test-env.sh
          ./tests/scripts/setup.sh

      - name: Run IO Tests
        run: ./tests/io/run_io_tests.sh

      - name: Run K3s Tests
        run: ./tests/k3s/run_k3s_tests.sh

      - name: Run All Tests
        run: ./tests/run_all_tests.sh
```

---

## 最佳实践

### 1. 测试命名规范

- 测试 ID: `<PREFIX>-NNN` (如 IO-001, K3S-001)
- 测试函数: `test_case_n()` (如 test_case_1)
- 测试脚本: `test_<feature>_<scenario>.sh`

### 2. 测试隔离

- 每个测试独立运行，不依赖其他测试
- 使用唯一的容器名称（如 `test-$$-$(date +%s)`）
- 测试结束后清理资源

### 3. 超时处理

```bash
# 使用 timeout 命令
timeout 30 ctr task attach test-container || {
    echo "Test timed out"
    exit 1
}
```

### 4. 错误处理

```bash
# 使用 trap 确保清理
trap cleanup EXIT

cleanup() {
    echo "Cleaning up..."
    ctr task delete -f test-container 2>/dev/null || true
    ctr container delete test-container 2>/dev/null || true
}
```

### 5. 日志记录

```bash
# 记录详细日志
LOG_FILE="$TEST_LOG_DIR/test-$(date +%Y%m%d-%H%M%S).log"
exec > >(tee -a "$LOG_FILE")
exec 2>&1
```

---

## 附录

### A. 测试模板

完整的测试脚本模板请参考 `tests/lib/test_template.sh`。

### B. 工具函数

常用的工具函数定义在 `tests/lib/test_utils.sh`:

```bash
# 远程执行
remote "command"

# 清理容器
cleanup_container <name>

# 等待容器就绪
wait_for_container <name> <timeout>

# 检查容器状态
get_container_status <name>

# 断言函数
assert_equals <expected> <actual> <message>
assert_contains <haystack> <needle> <message>
```

### C. 参考文档

- [testing-guide.md](testing-guide.md) - 详细测试指南
- [k8s-integration.md](k8s-integration.md) - Kubernetes 集成
- [quick-start.md](quick-start.md) - 快速开始

---

**问题反馈**: 提交 Issue 到 [MCS 项目](https://atomgit.com/openeuler/mcs/issues)
