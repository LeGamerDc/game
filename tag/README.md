## tag 包

层级标签系统：在 `DB` 构造期把点分层级字符串（如 `a.b.c`）编译为紧凑的 `uint16` 标识（`Key`），运行期在 `Tag` 中以"祖先闭包引用计数"维护匹配，并用可嵌套的表达式树 `Query` 做过滤。设计对齐 Unreal GameplayTags（其 NetIndex 同为 `uint16`）。

### 核心概念

- **Key**: tag 的紧凑标识，`uint16`。零值 `InvalidKey` 表示"无 tag / 无父级"，真实 id 从 1 开始（故单个 DB 最多 65535 个 tag，超出 `Build` 报错）。引用计数用 `int16`（单实体内的 grant/覆盖数，量级很小）。
- **DB**: 不可变的标签字典。由 `Build` 一次性构造，构造后只读，因此可在多 goroutine 间无锁共享。
  - 大小写不敏感（`A.B` 与 `a.b` 同一个 tag）。
  - 自动补全祖先：注册 `a.b.c` 会同时注册 `a.b` 与 `a`。
  - 畸形串（空串、首尾点、`a..b` 空段）会让 `Build` 报错。
  - id 按排序后的字典序分配，因此对同一 tag 集合是确定的、跨进程一致的。
- **Tag**: 单个实体的标签集合，内部维护两套引用计数：
  - `explicit`：每个 tag 被显式 grant 的次数（= **精确**集合，`HasTagExact`；支持多来源 grant，最后一个撤销才消失）。
  - `closure`：每个 tag 被多少个"互异显式后代或自身"覆盖（= **层级**集合，`HasTag`）。
  - 二者增量维护：`AddTag`/`RemoveTag` 只在显式 tag 0↔1 跃迁时走一条祖先链（O(depth)），不全量重建。
- **Query**: 编译后的标签查询，可重复匹配。

### 匹配语义

- `HasTag(t)`：**层级**。拥有 `a.b.c` 即 `HasTag(a.b)`、`HasTag(a)` 为真；但只拥有父级 `a` 不会让 `HasTag(a.b)` 为真。
- `HasTagExact(t)`：**精确**。只有被显式 grant 的 tag 才为真，不走祖先闭包。

### 快速开始

```go
db, err := tag.Build(slices.Values([]string{"a.b.c", "x.y", "z"}))
// db 构造后不可变、只读、并发安全

abc, _ := db.Lookup("a.b.c")
ab, _ := db.Lookup("a.b")

var t tag.Tag
t.AddTag(db, abc) // 加最细粒度即可，祖先自动生效

t.HasTag(ab)        // true（祖先闭包）
t.HasTagExact(ab)   // false（非显式 grant）

// 便捷查询：all（必须全有）/ none（必须全无）/ some（至少其一）
q, _ := tag.NewQuery(db, []tag.Key{ab}, nil, nil)
t.Match(q) // true

// 计数移除：多次 Add 需对应 Remove 到 0 才剔除
t.RemoveTag(db, abc)
```

### 查询表达式树

`NewQuery(all, none, some)` 只是常见形态的便捷壳，等价于
`And(AllTags(all), NoTags(none), AnyTags(some))`（空子句省略）。
完整能力是可任意嵌套的布尔表达式树：

```go
// (有 abc 且有 x) 或 (有 abd 且有 z)，且不含 stun
q, _ := tag.NewQueryExpr(db, tag.And(
    tag.Or(
        tag.And(tag.AllTags(abc), tag.AllTags(x)),
        tag.And(tag.AllTags(abd), tag.AllTags(z)),
    ),
    tag.Nor(tag.AllTags(stun)),
))
```

- 叶子（操作一组 tag）：`AllTags` / `AnyTags` / `NoTags`，以及精确变体 `AllTagsExact` / `AnyTagsExact` / `NoTagsExact`。
- 复合（操作一组子表达式）：`And` / `Or` / `Nor`。

### 归一化与"不可满足"语义

`NewQuery*` 在构造期只做**廉价且恒正确**的化简：

1. 叶子内部去重；层级收敛（`AllTags` 留最具体，`AnyTags`/`NoTags` 留最宽）。**精确叶子不做层级收敛**（祖先与后代在精确语义下互不蕴含）。
2. 常量折叠：空叶子折成常量；`And`/`Or`/`Nor` 对常量子表达式折叠。

它**不**做跨兄弟节点的可满足性求解。一个注定不满足的查询**不是构造错误**——它只是永远不命中（根折叠为/求值为 false）。唯一的构造错误是结构性的（`nil` DB）。非法 `Key` 也按此处理：在 `all` 中导致永不命中，在 `none` 中无害，无需特判。
