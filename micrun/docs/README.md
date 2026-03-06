# MicRun 文档

MicRun 是一个 containerd shim v2 运行时，用于管理 RTOS (Zephyr, UniProton) 容器。

## 快速开始

[快速入门](quick-start.md) - 从零开始部署 MicRun

## 用户文档

- [Kubernetes 集成](user/kubernetes.md) - 在 Kubernetes 中使用 MicRun
- [故障排查](user/troubleshooting.md) - 常见问题排查
- [性能调优](user/performance-tuning.md) - 性能优化指南

## 参考文档

- [注解参考](reference/annotations.md) - 容器注解列表
- [配置参考](reference/configuration.md) - 运行时配置说明
- [API 参考](reference/api-reference.md) - Containerd API 接口
- [资源映射](reference/resources.md) - CPU/内存资源映射规范

## 内部文档

- [IO 系统](internals/io-system.md) - IO 系统架构设计
- [日志系统](internals/logging.md) - 日志系统设计
- [状态管理](internals/state-management.md) - 状态持久化和恢复
- [Sandbox 验证](internals/sandbox-validation.md) - Sandbox 状态验证机制

## 测试指南

- [测试框架](../tests/README.md) - 测试方法和用例
- [IO 测试](../tests/io/README.md) - IO 系统测试
- [K3s 测试](../tests/k3s/README.md) - K3s 集成测试
