# MicRun 参考文档

本目录提供“当前代码如何工作”的接口与配置说明。

## 文档列表

- [annotations.md](./annotations.md)
  MicRun 支持的注解、优先级和使用方式。

- [configuration.md](./configuration.md)
  运行时配置来源、文件位置、主要键值。

- [api-reference.md](./api-reference.md)
  当前核心接口与主要调用链的代码落点。

- [resources.md](./resources.md)
  CPU / 内存映射、cpuset 归一化、VCPU 回退策略。

## 使用建议

- 想写 Pod/Container 清单：先看 `annotations.md`
- 想调整默认值或 drop-in：看 `configuration.md`
- 想从代码入口理解主要接口：看 `api-reference.md`
- 想排资源行为：看 `resources.md`
