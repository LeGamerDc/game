# Todo — 当前任务执行清单

Last Updated: 2026-03-31

## 当前任务：GDC 投稿准备 — 先行工作分析与价值评估

### 已完成步骤

- [x] 读取 memory 文件，了解项目上下文
- [x] 读取设计文档（parallel.md, scheduler.md, adaptation_guide.md, world.go）
- [x] 分析用户提供的 7 篇先行工作论文（Cordeiro 2007, Abdelkhalek 2004, Atomic Quake 2009, QuakeTM 2009, SynQuake 2010, Mohebali 2014, Zamith 2015）
- [x] 搜索 2015–2025 年间的新相关工作（Redmond OOPSLA 2025, SpacetimeDB, SpatialOS, Unity DOTS, Bevy, UE5 等）
- [x] 调研 GDC Programming/Technology Track 历史同类演讲
- [x] 产出综合覆盖矩阵：10 个核心特征 × 12+ 已知工作
- [x] 产出新颖性结论：10 项新颖贡献，0 项被完整覆盖
- [x] 产出 GDC 发表价值评估：Competitive，填补主题空白
- [x] 产出建议的 Related Work 叙事结构和 GDC 提交策略
- [x] 生成分析文档 `docs/papers/novelty_and_value_analysis.md`
- [x] 生成详细先行工作分析 `docs/references/prior_work_analysis.md`
- [x] 生成近年新工作分析 `docs/references/prior_art_novelty_analysis.md`

### 待完成步骤

- [ ] 补充搜索已知盲点（中文学术文献、专利库、NetGames/FDG/I3D 会议）
- [ ] 检查 Redmond OOPSLA 2025 论文的 Related Work 部分，追踪引用链
- [ ] 设计并实现性能 Benchmark（串行 vs 并行 vs 自适应，不同 entity 数量）
- [ ] 构建至少一个完整 combat path 的端到端 demo
- [ ] 与简单方案（单线程串行、goroutine-per-entity）的对比实验

## Notes

### 新颖性核心结论
- 7 篇先行工作 + 12+ 近年工作全部分析完毕
- 最高威胁：SynQuake 2010（2.5/5），stage-based + barrier 但无 ownership/effect 代数
- 需重点 position：Cordeiro 2007（概念先驱）、Redmond OOPSLA 2025（同期不同路径）
- F3（ownership）、F4（effect 代数）、F5（串/并自适应）、F9（107 条验证）、F10（适配方法论）完全无先例

### GDC 价值核心结论
- GDC 2027 track 已从 "Programming" 改名为 "Game & Production Technology"
- 服务器端 tick 并行化在 GDC 历史上是主题空白
- SpacetimeDB (GDC 2025) 是最近的服务器端并发演讲，但走数据库事务路径
- P0 差距：benchmark + 端到端 demo