# Todo — 当前任务执行清单

Last Updated: 2026-04-03

## 最近完成：Signal/Effect 代数化调研

### 已完成步骤

- [x] 读取 memory 文件，了解项目上下文
- [x] 读取 signal/effect 相关代码：world.go、scheduler.go、scheduler_parallel.go、scheduler_serial.go
- [x] 读取设计文档：parallel.md、scheduler.md 的 signal/effect 相关章节
- [x] 调研 Signal 代数化：游戏引擎事件合并模式、代数结构（monoid/semilattice/group/CRDT）、FRP 事件流代数
- [x] 调研 Rx operator 代数语义、Event Sourcing compaction、Actor Model 消息批量、Process Algebra 事件组合
- [x] 调研 Effect 代数组合在实际游戏中的使用情况：Unreal GAS、Overwatch、SpacetimeDB、Bevy、Unity DOTS
- [x] 确认结论：Effect/Signal 代数组合（框架级预合并）不做
- [x] 澄清 F4 commutativity 精确含义：容忍性，非数学严格交换律
- [x] 更新设计文档 parallel.md
- [x] 更新 memory 文件

## 当前无活跃任务

下一步可选方向（参见 tasks.md Active）：
- 性能 Benchmark
- 端到端 Combat Path Demo
- GDC 投稿准备