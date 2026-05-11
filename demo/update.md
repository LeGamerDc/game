# Demo Combat 实现更新

Last Updated: 2026-05-09

## 2026-05-09

- 已按 `docs/design/demo_combat_framework.md` 的默认语义实现首版 `demo/combat` 与 `demo/scenario`：
  - 普通攻击在 fire 后进入 cooldown，技能队列可在普攻 CD 内接管。
  - 技能 CD 从 cast commit 时间开始计算。
  - 弹道命中只检查目标仍存在且 `Alive/Targetable`，不重新检查距离。
  - `AttackRange` / `AttackSpeed` 已加入 `demo/cfg/attr.toml` 并生成到 `demo/combat/demo_attr.go`。
  - `ProjectileSpeed` 暂保留为普通攻击/技能配置，不作为 attribute。
  - Unit 使用基于 ref 的私有 deterministic RNG。
- 串行 scheduler 路径下，`Emit` / `Publish` 可能 inline 触发递归 Think/Apply。实现规则调整为：在发出 self signal 或可能回流到 source 的 effect 前，必须先提交 owner-local 私有状态变更，避免递归 Think 看到半提交状态。
- 到期队列类 private state（pending impact、buff periodic/expire）必须先从队列中移除或推进 deadline，再发布 effect；否则串行回流 signal 可能导致同一 pending item 被重复处理。
- MVP 的 n x n 创建是 tick 外部的 scenario 初始化流程，暂未把 spawn/despawn 建模为投递给 `RefWorld` 的 effect；后续如果要演示运行时创建/销毁，需要补 World/SerialRef apply-only dispatch。
