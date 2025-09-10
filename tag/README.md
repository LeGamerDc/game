## tag 包

轻量级层级标签系统：用 `DB` 将点分层级字符串（如 `a.b.c`）编译为紧凑的 `int16` 标识，并在 `Tag` 中以祖先闭包缓存的方式进行匹配与过滤。

### 核心概念
- **DB**: 负责把字符串标签编译为 `int16`，并维护父子关系。例：`a.b.c` 的父为 `a.b`，`a.b` 的父为 `a`，`a` 的父为 `-1`。
- **Tag**: 某个实体的标签集合，内部维护计数与“包含自身及所有祖先”的缓存。
- **Query**: 过滤条件：
  - `all`: 必须全部命中
  - `none`: 必须全部不命中
  - `some`: 至少命中一个（若为空则不参与判断）

### 快速开始
```go
db := tag.NewDB()

// 编译（注册）分层标签，返回 int16 标识
aid := db.Compile("a")
abid := db.Compile("a.b")
abcid := db.Compile("a.b.c")

var t tag.Tag

// 添加最细粒度的标签即可，祖先会自动生效（HasTag 对 a、a.b、a.b.c 都为 true）
t.AddTag(abcid, db)

_ = t.HasTag(aid)   // true
_ = t.HasTag(abid)  // true
_ = t.HasTag(abcid) // true

// 匹配：all/none/some 组合
q := tag.Query{ // 同包可直接构造（字段为非导出）
    all:  []int16{abid},   // 必须包含 a.b
    none: []int16{},
    some: []int16{},
}
_ = t.Match(q) // true

// 计数移除：多次 Add 后需对应 Remove 到 0 才会从缓存剔除
t.RemoveTag(abcid, db)
```

### 匹配规则说明
- 缓存包含“自身及所有祖先”，因此拥有 `a.b.c` 等价于同时拥有 `a.b` 与 `a`。
- 只拥有父级不代表拥有子级：若仅添加 `x`，则 `x.y` 不被视为命中。
- `Match` 逻辑：
  1. 若有任意 `all` 不命中 => false
  2. 若有任意 `none` 命中 => false
  3. 若 `some` 非空 => 只要命中其中一个 => true；否则 false
  4. 若 `some` 为空且前两步未失败 => true

