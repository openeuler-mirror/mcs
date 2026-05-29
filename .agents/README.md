# MICA Agent Knowledge Hub

这个目录存放面向 AI agent 的仓内本地知识入口，帮助 agent 在本仓中处理 MICA 相关分析、开发与排障任务。

## 职责分工
- `AGENTS.md`：定义 agent 的工作协议、入口顺序、任务分类、输出要求与更新规则
- `skills/*/SKILL.md` 与 `references/`：承载各问题域和工作流的具体知识内容
- `tools/*.py`：面向 agent workflow 的辅助工具

## 入口说明
- 需要判断 agent 应该怎么工作、先看什么、输出什么时，先读 `AGENTS.md`
- 需要进入具体问题域或工作流时，再读对应 `skills/*/SKILL.md` 与 `references/`
- 本文件不重复定义详细工作流路由，具体入口顺序以 `AGENTS.md` 为准

## 编写规则
- 仓内路径统一使用相对路径。
- 公开 skill 内容中不要写机器相关的绝对路径。
- 对外部依赖，只写仓库名称、推荐 clone 方式、建议查看目录。
- `SKILL.md` 保持精简，长文说明放到 `references/`。

## 目录结构
- `skills/<skill-name>/SKILL.md`
- `skills/<skill-name>/references/*.md`
- `tools/*.py`：面向 agent workflow 的辅助工具

外部组件源码获取等跨 workflow 的通用辅助能力也集中放在 `tools/*.py` 中维护。

## 维护要求
凡是会影响 MICA 架构、生命周期、通信机制或调试方法的改动，都应在同一任务中同步更新对应 skill 与 reference。
