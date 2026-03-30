# Todo — 当前任务执行清单

Last Updated: 2026-03-29

当前任务：Scheduler Review 反馈修复

- [x] 分析 hasWork O(C×B) 扫描问题 → 确认 signalRead 按 source thread 索引，非消费端索引；用户确认不需要优化
- [x] 分析 buf 无限膨胀问题（signalGroupBufs/groupBufs map 跨 block 泄漏僵尸 key）
- [x] 设计 sort-based flat buffer 分组方案替代 map 分组
- [x] 实现 `refValInbox[S]` / `refValArrangement[E]` 适配器类型
- [x] 实现 `collectBuf[V]` wrapper（头部 CacheLinePad 隔离）
- [x] 重写 `thinkWorker`：flat buffer 收集 → sort by ref → 线性分组调用 Think
- [x] 重写 `applyWorker`：flat buffer 收集 → sort by ref → 线性分组调用 Apply
- [x] 替换 Scheduler 字段：`signalGroupBufs`/`groupBufs` → `thinkCollectBuf`/`applyCollectBuf`
- [x] 更新 NewScheduler 初始化
- [x] 编译通过 + 26 个测试全部通过（含 race detector）
- [ ] 更新 memory.md 反映新的设计决策

## Notes

- **hasWork 不优化**：用户确认 signalRead 按 source thread 索引的分布特征使得优化不必要。O(C×B)=O(685) 在实践中开销可忽略。
- **sort 替代 map 的核心洞察**：map 天然在 key 维度上泄漏状态，而 block 级别的分组是独立的、不应跨 block 积累。sort-based flat buffer 天然尊重 block 独立性——`flatBuf[:0]` 完全重置，无僵尸条目。
- **cache line 策略**：不假定 cache line = 64 bytes（ARM 等平台已是 128）。识别 thread list 结构，统一用头部 `cpu.CacheLinePad` 隔离，不加尾部 pad。
- **已有 CacheLinePad 的 thread list 结构**：`blockCollector`、`timerCollector`、`collectBuf`（新增）。

## 下一步

- 更新 memory.md
- 继续 tasks.md 中的其他 Active 任务