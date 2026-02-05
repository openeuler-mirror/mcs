# MicRun 文档

MicRun 是一个 containerd shim v2 运行时，用于管理 RTOS (Zephyr, UniProton, LiteOS) 容器。

## 快速开始

- [快速入门](quick-start.md) - 快速上手指南

## 用户文档

面向使用 MicRun 部署和管理 RTOS 容器的用户。

- [Kubernetes 集成](user/kubernetes.md) - 在 Kubernetes 中使用 MicRun
- [故障排查](user/troubleshooting.md) - 常见问题排查
- [性能调优](user/performance-tuning.md) - 性能优化指南

## 参考文档

面向需要详细了解配置和 API 的用户。

- [注解参考](reference/annotations.md) - 容器注解完整列表
- [配置参考](reference/configuration.md) - 运行时配置说明
- [API 参考](reference/api-reference.md) - Containerd API 接口
- [资源映射](reference/resources.md) - CPU/内存资源映射规范

## 内部文档

面向开发者和贡献者，了解 MicRun 的内部设计。

- [IO 系统](internals/io-system.md) - IO 系统架构设计
- [日志系统](internals/logging.md) - 日志系统设计
- [状态管理](internals/state-management.md) - 状态持久化和恢复
- [Sandbox 验证](internals/sandbox-validation.md) - Sandbox 状态验证机制

## 开发指南

面向贡献者和测试人员。

- [测试指南](development/testing.md) - 如何测试 MicRun
- [K8s 测试计划](development/k8s-test-plan.md) - Kubernetes 集成测试
- [测试框架](development/test-framework.md) - 测试框架说明

## 特性设计

面向想深入了解特定功能的用户和开发者。

- [特性设计索引](features/README.md) - 各特性设计文档导航

## 深入分析 (study/)

历史设计分析文档，保留作为参考。

- [Runc vs MicRun](study/runc-vs-micrun.md) - 架构对比
- [Shim 开发笔记](study/shim-dev-notes.md) - 开发笔记
- [一次性命令分析](study/oneshot-command-analysis.md) - 命令分析
- [Start/Delete 分析](study/shim-start-delete-analysis.md) - 命令机制
- [Namespace 分析](study/namespace-env-analysis.md) - 环境变量分析
- [Sandbox 恢复](study/restore-sandbox-analysis.md) - 状态恢复分析

## 归档文档 (archive/)

已废弃或过时的文档，保留作为历史参考。

- [归档索引](archive/README.md) - 归档文档说明

## 其他文档

- [问题追踪](issues/micad-tty-timeout.md) - TTY 超时问题分析
