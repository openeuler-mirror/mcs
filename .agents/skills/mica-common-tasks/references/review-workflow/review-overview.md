# 评审类任务工作流

## 1. 任务目标

评审类任务面向 PR、patch 或新代码检视，目标是判断改动是否符合 MICA 架构边界、模块交互、生命周期语义、通信语义和平台适配原则。

典型任务包括：
- 检视新增 service 是否合理
- 检视新 RTOS 或新 pedestal 适配是否完整
- 检视 lifecycle、communication、debugging 相关代码是否破坏既有语义
- 检视 shared memory、IRQ、notify、cache、resource table 相关改动是否有平台风险
- 检视文档、测试、示例配置是否同步

## 2. 评审入口顺序

处理评审类任务时，优先按下面顺序建立评审框架：

1. AtomGit PR 准备
    - 当用户只提供 `mcs PR #xx`、`PR #xx` 或 AtomGit PR URL 时，先使用 `.agents/tools/atomgit_pr_context.py` 拉取只读 PR 上下文
    - 默认仓库为 `openEuler/mcs`
    - 输出文件为 `tmp/mcs_pr_<number>_info.json`
    - 该步骤不 checkout、不修改工作树、不提交评论

2. `../../../mica-overview/SKILL.md`
    - 建立整体架构边界

3. 与改动直接相关的 domain skill
     - Linux/master：`../../../mica-linux-master/SKILL.md`
     - RTOS/client：`../../../mica-rtos-client/SKILL.md`
     - 生命周期：`../../../mica-lifecycle/SKILL.md`
     - 通信：`../../../mica-communication/SKILL.md`
     - pedestal：`../../../mica-pedestals/SKILL.md`
     - 调试工作流：`../debugging-workflow/debugging-overview.md`

4. 需要快速定位关联代码时进入对应 domain 文档：
    - 生命周期：`../../../mica-lifecycle/references/lifecycle-overview.md`
    - 通信服务：`../../../mica-communication/references/communication-overview.md`
    - 跨层边界：`../debugging-workflow/boundary-diagnosis.md`

5. Maintainer 确认与评论提交
   - agent 先把检视意见反馈给 maintainer
   - maintainer 决定哪些意见需要回复到 PR
   - 只有在 maintainer 明确确认后，agent 才能调用 AtomGit API 提交评论

## 3. AtomGit PR 上下文获取

当 maintainer 说“mcs 仓 PR #xx”时，执行：

```sh
python3 .agents/tools/atomgit_pr_context.py --pr <number>
```

前提：
- 环境变量 `ATOMGIT_TOKEN` 已设置
- 默认 API 地址为 `https://api.atomgit.com`
- 默认目标仓库为 `openEuler/mcs`

也可以从 URL 自动解析仓库和 PR 编号：

```sh
python3 .agents/tools/atomgit_pr_context.py --url https://atomgit.com/openEuler/mcs/pull/<number>
```

输出 JSON 包含：
- PR 标题、作者、状态、base/head 分支和 head sha
- changed files、patch、带行号的 head 文件内容
- commits
- comments 和 unresolved comment 统计

评审时优先读取：
- `pr.changed_files[].filename`
- `pr.changed_files[].patch`
- `pr.changed_files[].content`
- `commits`
- `comments`

如果 PR 较大，可先按 `filename` 分类，再只读取相关文件的 patch 和 content。

## 4. 批判式检视原则

检视新代码时，agent 应保持批判式检视心理，不应主动替新代码寻找合理化解释。

核心原则：
- 默认假设新代码可能破坏既有语义、边界或平台约束，必须由证据排除风险
- 对失败路径、资源释放、状态转换、并发、边界值、格式化输出、类型转换和跨层契约保持敏感
- 不因改动规模小、作者意图明确或常见平台上“看起来能工作”而降低问题等级
- 不把 maintainer 的最终判断前置为 agent 的结论
- 对不能确认的问题，应以风险、证据缺口或开放问题形式反馈给 maintainer

agent 的职责是提出可审查的问题和依据；是否采纳、如何回复以及回复语气由 maintainer 决定。

## 5. 架构边界检查

评审时优先确认：

- Linux/master 是否只承担控制面、生命周期、状态可见性和本侧 service 管理职责
- RTOS/client 是否只承担 client runtime、endpoint、receiver、service thread 与 OS 适配职责
- pedestal 是否承担平台差异、shared memory、IRQ、notify、resource table 或 backend 适配职责
- OpenAMP/libmetal 机制是否被正确封装，而不是被上层随意绕过

## 6. 生命周期语义检查

涉及 create/start/stop/remove/status 的改动，应确认：

- 状态转换是否清晰
- stop 与 remove 的边界是否被混淆
- restart 前提是否仍成立
- `Running` 是否被误当成 service ready 或业务可用
- 失败路径是否释放了对应资源

## 7. 通信与 service 语义检查

涉及 TTY、UMT、RPC、GDB 或 RPMsg 的改动，应确认：

- endpoint 名称与 match 规则是否稳定
- name service、bind callback、first message 的职责是否混淆
- endpoint ready、service ready、业务可用性是否区分清楚
- buffer hold/release、共享内存、copy-message 区域是否安全
- Linux/master 与 RTOS/client 两侧语义是否对齐

## 8. pedestal 与平台风险检查

涉及 pedestal、硬件平台或底层适配的改动，应确认：

- shared memory 布局是否仍与 resource table、vring、service 数据区兼容
- IRQ/notify 路径是否和目标平台匹配
- cache、I/O region、phys/virt 转换是否有明确语义
- OpenAMP/libmetal 下钻边界是否清晰
- 不同 pedestal 是否被不必要地耦合

## 9. 测试与文档检查

评审时还应确认：

- 是否有对应使用或调试验证路径
- 是否更新配置示例或测试说明
- 是否需要更新 `.agents/skills/**` 中的相关文档
- 是否影响现有用户可见行为或兼容性

## 10. 评审类输出要求

评审类输出应以 findings 为主，按严重程度排序，并包含：

- 文件或模块位置
- 问题描述
- 风险说明
- 建议修复方向
- 测试或文档缺口

没有发现问题时，应明确说明未发现阻塞问题，并列出残余风险或未覆盖验证项。

## 11. Maintainer 确认后的 AtomGit 评论提交

agent 给出检视意见后，不应自动提交评论。提交 PR 评论必须满足：

- maintainer 明确选择需要提交的检视意见
- maintainer 明确认可最终评论文本
- 评论只包含 maintainer 决定要反馈到 PR 的内容

建议流程：

1. agent 输出 findings，按严重程度排序。
2. maintainer 指定要提交的意见，例如“提交第 1 条”或“把第 1 条改成 warning 后提交”。
3. agent 生成评论文件到 `tmp/` 下，例如 `tmp/mcs_pr_<number>_comment.md`。
4. agent 使用 dry-run 展示最终评论内容。
5. maintainer 确认后，agent 执行提交命令。

提交 PR 级评论：

```sh
python3 .agents/tools/atomgit_pr_context.py --pr <number> --comment-file tmp/mcs_pr_<number>_comment.md
```

提交前预览：

```sh
python3 .agents/tools/atomgit_pr_context.py --pr <number> --comment-file tmp/mcs_pr_<number>_comment.md --dry-run
```

约束：
- 不在 maintainer 确认前提交评论
- 不提交未被 maintainer 选择的 findings
- 不在评论中暴露本地路径、token、临时分析过程或内部技能文档路径
- 不通过 checkout、切分支或修改工作树来完成评论提交
