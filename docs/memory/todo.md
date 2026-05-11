# Todo — 当前任务执行清单

Last Updated: 2026-05-10

## 当前任务：BT Game Developer 投稿准备

- [x] 读取 memory、Game Developer 风格调研、BT 设计稿、审计稿和关键实现
- [x] 确认文章主 claim：stack-first / continuation stack runtime，而非行为树语义创新
- [x] 设计符合 Game Developer technical breakdown 的文章组织结构
- [x] 规划图示优先级，减少正文伪代码
- [x] 使用 imagegen 生成投稿头图并拷入 `docs/papers/assets/bt_stack_runtime/header-server-ai.png`
- [x] 生成本地 16:9 PNG 技术图：root tick 对照、active stack、resume/unwind、AlwaysGuard、parallel roots、event wake、cancel unwind
- [x] 按组织稿撰写英文 Markdown 投稿正文 `docs/papers/bt_stack_runtime_submission.md`
- [ ] 补传统 root tick / memory composite 对照 benchmark
- [ ] 补 event wakeup / deep running leaf / allocation 数据
- [ ] 人工审阅投稿标题、语气、图示密度和 prior art 边界表达

## Notes

- 正文应围绕现代 BT 特性适配案例展开：composite/decorator frame、AlwaysGuard sub-root、parallel child roots、event dispatch、discrete wakeup、cancel unwind。
- 当前文章结构沉淀在 `docs/papers/bt_stack_runtime_article_outline.md`，英文正文在 `docs/papers/bt_stack_runtime_submission.md`。
- 当前正文明确避免性能倍数宣称；如果后续决定把它变成性能导向文章，需要补对照 benchmark 与 allocation/pprof 解释。
