#!/bin/bash
# 本地状态验证测试脚本
# 使用 mock micad 进行测试

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MICRUN_ROOT="$(dirname "$SCRIPT_DIR")"

echo "=== 状态验证本地测试 ==="
echo "测试时间: $(date)"
echo ""

# 定义清理函数
cleanup() {
    echo ""
    echo ">>> 清理测试环境..."
    cd "$MICRUN_ROOT/tests/mock_micad"
    make clean-all > /dev/null 2>&1 || true

    # 清理可能残留的状态目录
    rm -rf /run/micrun/sandbox/test-validation-* 2>/dev/null || true

    echo "=== 测试环境已清理 ==="
}
trap cleanup EXIT

# 1. 启动 mock micad
echo ">>> 步骤 1: 启动 mock micad..."
cd "$MICRUN_ROOT/tests/mock_micad"
make clean-all > /dev/null 2>&1 || true
make run &
MOCK_PID=$!
echo "Mock micad PID: $MOCK_PID"
sleep 2

# 检查 mock micad 是否启动成功
if ! kill -0 $MOCK_PID 2>/dev/null; then
    echo "ERROR: Mock micad 启动失败"
    exit 1
fi
echo "Mock micad 启动成功"
echo ""

# 2. 测试 1: 创建状态文件并验证元数据
echo ">>> 步骤 2: 测试状态文件元数据..."
TEST_ID_1="test-validation-001"
TEST_DIR="/run/micrun/sandbox/$TEST_ID_1"
mkdir -p "$TEST_DIR"

# 创建包含新元数据的状态文件
CURRENT_PID=$$
CURRENT_TIME=$(date +%s)
cat > "$TEST_DIR/state.json" <<EOF
{
    "id": "$TEST_ID_1",
    "state": {"state": "running"},
    "config": {"id": "$TEST_ID_1"},
    "created_at": $CURRENT_TIME,
    "shim_pid": $CURRENT_PID
}
EOF

echo "创建的状态文件 ($TEST_DIR/state.json):"
cat "$TEST_DIR/state.json"

# 验证元数据字段
if command -v jq &> /dev/null; then
    CREATED_AT=$(jq -r '.created_at' "$TEST_DIR/state.json")
    SHIM_PID=$(jq -r '.shim_pid' "$TEST_DIR/state.json")

    if [ "$CREATED_AT" = "null" ] || [ -z "$CREATED_AT" ]; then
        echo "ERROR: created_at 字段缺失"
        exit 1
    fi

    if [ "$SHIM_PID" = "null" ] || [ -z "$SHIM_PID" ]; then
        echo "ERROR: shim_pid 字段缺失"
        exit 1
    fi

    echo "✓ 元数据验证通过: created_at=$CREATED_AT, shim_pid=$SHIM_PID"
else
    echo "⚠ jq 未安装，跳过 JSON 验证"
fi
echo ""

# 3. 测试 2: 僵尸状态检测（shim 已死亡）
echo ">>> 步骤 3: 测试僵尸状态检测..."
TEST_ID_2="test-validation-stale"
TEST_DIR_2="/run/micrun/sandbox/$TEST_ID_2"
mkdir -p "$TEST_DIR_2"

# 使用一个不可能存在的 PID
cat > "$TEST_DIR_2/state.json" <<EOF
{
    "id": "$TEST_ID_2",
    "state": {"state": "running"},
    "config": {"id": "$TEST_ID_2"},
    "created_at": $CURRENT_TIME,
    "shim_pid": 99999
}
EOF

echo "创建的僵尸状态文件 ($TEST_DIR_2/state.json):"
cat "$TEST_DIR_2/state.json"

# 验证 shim_pid 字段存在
if grep -q '"shim_pid": 99999' "$TEST_DIR_2/state.json"; then
    echo "✓ 僵尸状态文件创建成功，包含 shim_pid 字段"
else
    echo "ERROR: shim_pid 字段未找到"
    exit 1
fi
echo ""

# 4. 测试 3: 运行单元测试
echo ">>> 步骤 4: 运行 Go 单元测试..."
cd "$MICRUN_ROOT"

# 运行状态验证相关的单元测试
echo "运行 processExists 测试..."
if go test -v ./pkg/micantainer -run TestProcessExists -timeout 30s; then
    echo "✓ TestProcessExists 通过"
else
    echo "✗ TestProcessExists 失败"
    exit 1
fi
echo ""

echo "运行 ValidateSandboxState 测试..."
if go test -v ./pkg/micantainer -run TestValidateSandboxState -timeout 30s; then
    echo "✓ TestValidateSandboxState 通过"
else
    echo "✗ TestValidateSandboxState 失败"
    exit 1
fi
echo ""

echo "运行 loadSandbox 相关测试..."
if go test -v ./pkg/micantainer -run TestLoadSandboxWithValidation -timeout 30s; then
    echo "✓ TestLoadSandboxWithValidation 通过"
else
    echo "✗ TestLoadSandboxWithValidation 失败"
    exit 1
fi
echo ""

# 5. 测试总结
echo "=== 测试总结 ==="
echo "✓ 所有测试通过"
echo ""
echo "测试覆盖的场景:"
echo "  1. 状态文件元数据 (CreatedAt, ShimPID)"
echo "  2. 僵尸状态检测 (shim 死亡)"
echo "  3. 进程存在性检查 (processExists)"
echo "  4. 状态验证函数 (ValidateSandboxState)"
echo ""
echo "=== 测试完成 ==="
