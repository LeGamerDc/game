# Todo — 当前任务执行清单

Last Updated: 2026-04-03

## 最近完成：tag.Query 编译态优化

### 已完成步骤

- [x] 读取 memory 文件，确认当前项目上下文与协作约定
- [x] 读取 `tag/tag.go`、`tag/builder.go`、`tag/tag_test.go`、`tag/README.md`，确认 Query 语义与当前用法范围
- [x] 讨论 Query 优化方向，确认先做“构造期编译 + 运行时轻量分派”，暂不引入 bitset 方案
- [x] 实现 `NewQuery(db, all, none, some)`，在构造期完成 hierarchy 归一化、invalid tag 过滤、冲突检测与 `some` 冗余消除
- [x] 重构 `Tag.Match`，改为基于单 slice + boundary + kind mask 的匹配路径
- [x] 补充测试：hierarchy normalization、impossible query、`some` 被 `all` 保证、invalid tag
- [x] 更新 `tag/README.md` 与 memory 文件
- [x] 验证：`go test ./tag`、`go test ./...`

## 当前无活跃任务

下一步可选方向（参见 tasks.md Active）：
- 性能 Benchmark
- 端到端 Combat Path Demo
- GDC 投稿准备
