# docs/ 索引

> Agent 导航用。每个文件一行摘要 + 关键词。文档变更时同步更新。
>
> 本文件不替代 `memory/memory.md`（当前工作上下文），而是补充"所有文档在哪、是什么"。

Last Updated: 2026-04-25

---

## design/ — 设计稿

| 文件 | 摘要 | 关键词 |
|------|------|--------|
| `parallel.md` | 并行 tick 概念模型：BSP superstep、Logic=Owner 所有权、Think/Apply 两阶段、Effect 顺序无关性 | BSP, superstep, ownership, Think/Apply |
| `scheduler.md` | Scheduler 实现级设计：并发/串行双模式、block-based effect 收集、LPT 负载均衡、StagedState、计算分解约束 | block collector, LPT, StagedState, ScheduleMeta |
| `ability_system.md` | GAS 设计参考稿：完整 GAS 不再作为 `game/` 基础 package 落地，Attribute/Modifier 抽到 `attr/`，其余业务组合留给 demo | buff, modifier, attribute, ability |
| `adaptation_guide.md` | 107 条逻辑链路适配分类指导手册，6 大分类 + 5 步判定流程 | adaptation, 6 categories, migration |

## references/ — 调研与理论

| 文件 | 摘要 | 关键词 |
|------|------|--------|
| `parallel_theory.md` | 并行仿真理论笔记：Actor Model、BSP/Pregel、CRDT、Unity ECS、Orleans 等理论骨架与引用锚点 | Actor, BSP, Pregel, CRDT, ECS |
| `survey.md` | 并行 tick 数据分类调研：消息的 9 种基础类别（Query/Command/Delta/Patch…）、World/Logic shape 设计 | message categories, delta, patch, query |
| `think.md` | 早期思考笔记汇总：effect 作为 typed state transition、signal 作为 typed delivery item、hybrid shape 结论 | effect semantics, signal semantics |
| `gas_survey.md` | GAS 调研总结：UE GAS 与 Scheduler 框架的对接分析，9 条关键约束回顾 | UE GAS, Think/Apply mapping |
| `reactive_notifications_formal_models.md` | 形式化模型：并行游戏仿真中 reactive notification 的时序模型、正确性论证与订阅机制 | BSP event model, notification, subscription |
| `prior_art_novelty_analysis.md` | 2015–2025 先行工作新颖性分析：评估学术/工业实践对本框架核心特征（F1–F6）的覆盖度 | novelty, prior art, F1–F6 |
| `prior_work_analysis.md` | 先行论文逐篇对比：Cordeiro BSP Quake 等论文 vs 目标系统 10 个特征（F1–F10） | paper comparison, F1–F10 |
| `scheduler_analysis_prompt.md` | Scheduler 适配分析 prompt 模板：向 AI agent 传达框架语义，用于结构化评估业务逻辑适配性 | prompt template, analysis |
| `gamedeveloper_style_guide.md` | gamedeveloper.com 技术文章风格指南：投稿规范、字数建议、风格分析 | blog style, submission |

## papers/ — 博客与论文

| 文件 | 摘要 | 关键词 |
|------|------|--------|
| `blog_parallel_tick.md` | 博客初稿（面向 gamedeveloper.com）：No Locks, No Transactions — 用结构化所有权并行化 MMO 服务器 tick | blog, publication |
| `novelty_and_value_analysis.md` | GDC 发表价值综合分析：先行工作对比 + 新颖性评估 + 发表竞争力判断 | GDC, novelty, value |

## tmp/ — 过程产物 `[archived]`

> 以下文件是 GAS 调研和适配分析的中间产物，调研结论已沉淀到 `design/ability_system.md` 和 `design/adaptation_guide.md`。
> 作为历史参考保留，不再活跃更新。

| 文件 | 摘要 | 关键词 |
|------|------|--------|
| `research_attributes.md` | UE GAS 属性系统与 Modifier Pipeline 调研 | UE, Attribute, Modifier |
| `research_effects.md` | UE5 GameplayEffect 系统调研：Duration、Period、Stacking 模型 | UE, GameplayEffect, Stacking |
| `research_abilities.md` | UE GAS Ability 激活流程、Tag 控制、打断/互斥调研 | UE, Ability, Tag, ASC |
| `research_cues_targeting_others.md` | Gameplay Cues、Targeting System 与其他 GAS-like 系统对比 | UE, Cues, Targeting |
| `dota2_skills_analysis.md` | DOTA2 10 个代表性技能/机制的框架适配分析 | DOTA2, adaptation |
| `lol_skills_analysis.md` | LOL 10 个技能的框架适配分析 | LOL, adaptation |
| `wow_skills_analysis.md` | WoW 10 个技能/机制的框架适配分析 | WoW, adaptation |
| `summary_analysis.md` | 30 个经典游戏技能适配性总结：53% 直接适配、47% 需妥协、0% 无法适配 | summary, adaptation results |

## memory/ — 协作记忆

| 文件 | 摘要 |
|------|------|
| `memory.md` | 稳定上下文：当前状态、已确认决策、开放问题、关键文件索引 |
| `tasks.md` | 项目级任务注册表：所有被追踪的任务及其状态 |
| `todo.md` | 当前活跃任务的执行清单：具体步骤和进展 |

> memory/ 的维护规则见 `AGENTS.md`，此处不重复。

---

## 文档关系图

```text
                          Theory & Research
                          =================

  parallel_theory.md ──→ survey.md ──→ think.md
         │                                │
         ▼                                ▼
  ┌─────────────────────────────────────────────┐
  │              design/parallel.md             │  ← 概念设计
  └──────────────────────┬──────────────────────┘
                         │
                         ▼
  ┌─────────────────────────────────────────────┐
  │             design/scheduler.md             │  ← 实现设计
  └─────────────────────────────────────────────┘

                          GAS Chain
                          =========

  research_*.md (tmp/) ──→ gas_survey.md ──→ ability_system.md

                       Adaptation Chain
                       ================

  dota2/lol/wow_skills_analysis.md (tmp/)
         │
         ▼
  summary_analysis.md (tmp/) ──→ adaptation_guide.md

                      Publication Chain
                      =================

  prior_art_novelty_analysis.md ─┐
  prior_work_analysis.md ────────┼──→ novelty_and_value_analysis.md
  gamedeveloper_style_guide.md ──┘              │
                                                ▼
                                    blog_parallel_tick.md
```

---

## 维护规则

1. **新增或删除文档时**，必须同步更新本文件。
2. **摘要限一句话**，关键词 3–5 个，够 agent 判断"要不要读"即可。
3. **文档关系图**在链路变化时更新，保持准确。
4. `tmp/` 下的文件标注 `[archived]`；如有新的过程产物也放在 `tmp/` 并在此登记。
