# Todo — 当前任务执行清单

Task: 修复 Ref 空间歧义
Last Updated: 2026-03-28

## Steps

- [ ] 在 `en/world.go` 中定义 `RefNone` 常量
- [ ] 重新划分 Ref 空间，确保 Normal / World / Serial 三类互斥
- [ ] 添加 `IsValidRef` 判断函数
- [ ] 修正 `IsSerialRef` 使其不匹配 `RefWorld`
- [ ] 更新 `docs/design/parallel.md` 中相关描述
- [ ] 跑通现有测试

## Notes

- 当前 bug 核心：`IsSerialRef(RefWorld) == true`，三类 ref 不互斥。
- 需要明确互斥分区后，再处理 Publish 拆分（见 `tasks.md` 中下一项 Active 任务）。