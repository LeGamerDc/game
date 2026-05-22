# Todo — 当前任务执行清单

Last Updated: 2026-05-13

## 当前任务：无

## 最近完成：Sched + demo 性能验证

- [x] 读取 `memory.md`、`tasks.md`、`todo.md`
- [x] 复查 `demo/scenario` runner、grid、skills 与集成测试
- [x] 复查 `demo/combat` Unit、Ability、Buff、World staged query 与 sched 接入路径
- [x] 跑通当前验证：`go test ./...`、`go test -race ./demo/...`
- [x] 确认当前 `demo/scenario` 尚无 benchmark
- [x] 确认 benchmark 技能配置与测试矩阵
- [x] 编写 benchmark 专用技能配置
- [x] 编写串行/并发 Go benchmark
- [x] 运行初步 benchmark，记录不同 grid 规模的结果
- [x] 根据结果决定是否需要 profiler 或调整负载

## Notes

- 首轮已新增 `AddBenchmarkAbilities`：低 CD 单体、群体伤害、短周期 burn buff、被动追击，能够稳定产生 Think/Apply/Signal 回流负载。
- `BenchmarkGridCombatScheduler` 覆盖 16x16 / 32x32 / 84x84 grid，并显式强制 serial、parallel-4、parallel-8；84x84 表示约 7000 单位档位。
- 三轮短 benchmark 显示并发收益稳定：32x32 下 serial 约 12.9-13.1 ms/tick，parallel-4 约 3.25 ms/tick，parallel-8 约 2.83-2.85 ms/tick。
- 84x84（7056 units）三轮短 benchmark：serial 约 123-125 ms/tick，parallel-4 约 26.7-26.8 ms/tick，parallel-8 约 23.6-24.2 ms/tick。
- literal 7000x7000 会初始化 4900 万 units，当前机器上不应直接运行。
- 后续如继续做性能文章数据，应转入 `Demo benchmark 后续分析`，补 benchstat/pprof 与 allocation 热点分析。
