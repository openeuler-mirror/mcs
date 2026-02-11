# 特性设计文档

本目录包含 MicRun 各个特性的设计文档，面向想深入了解特定功能的开发者和用户。

## 特性列表

### 核心特性

| 特性 | 说明 | 文档 |
|------|------|------|
| **Sandbox 验证** | 状态持久化和验证机制 | [internals/sandbox-validation.md](../internals/sandbox-validation.md) |
| **IO 系统** | epoll 零 CPU 等待、EventBus | [internals/io-system.md](../internals/io-system.md) |
| **日志系统** | containerd 兼容、双模式输出 | [internals/logging.md](../internals/logging.md) |

### 相关文档

- [参考文档](../reference/) - 配置和 API 参考
- [开发指南](../development/) - 测试和贡献指南
