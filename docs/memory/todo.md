# Todo — 当前任务执行清单

Last Updated: 2026-04-06

## 当前任务：性能 Demo 开发：验证 scheduler 并行性能

### 准备

- [ ] 确定 demo 场景设计（entity 数量级、交互密度、tick 复杂度）
- [ ] 确定需要采集的性能指标（throughput、latency、scaling curve 等）
- [ ] 搭建 benchmark 基础框架（harness、数据采集、输出格式）

### 实现

- [ ] 实现串行 baseline demo
- [ ] 实现并行模式 demo
- [ ] 实现自适应切换 demo
- [ ] 不同 entity 数量的扩展性测试（scaling curve）

### 分析与产出

- [ ] 收集性能数据，生成对比图表
- [ ] 分析瓶颈与优化空间
- [ ] 将结果整理为可用于博客投稿的数据支撑

### Notes

- 博客初稿已完成：`docs/papers/blog_parallel_tick.md`，待性能数据验证后再投稿
- 目标：用实际数据验证 scheduler 并行设计的性能优势，为 gamedeveloper.com 投稿提供说服力
- Backlog 中的"性能 Benchmark"任务与本任务合并执行