# 先行工作对比分析：游戏服务器并行化论文 vs 目标系统

Last Updated: 2025-07-29

## 目标系统核心特征速查

| # | Feature | Key Keyword |
|---|---------|-------------|
| F1 | BSP superstep 执行模型：Think → barrier → Apply → barrier → swap，每 tick 多轮 | Multi-round BSP |
| F2 | Think/Apply 两阶段：Think 读快照+写私有+产出 typed effect/signal；Apply 按 owner 聚合 | Two-phase R/W separation |
| F3 | Logic=Owner 所有权模型：调度/effect 投递/写权限全部绑定 owner | Ownership model |
| F4 | Typed Effect 代数性质：显式类型化结构，要求 commutativity，非 closure | Typed commutative effects |
| F5 | Signal 双缓冲传递：signalRead/signalWrite swap 实现 superstep 间无锁传递 | Double-buffered signals |
| F6 | Block-based Effect Collector：per-thread per-block 收集，CacheLinePad 隔离，sort-based 分组 | Cache-aware collection |
| F7 | 自动串/并行模式切换：基于工作量阈值自适应，串行是终态，truly inline 递归执行 | Adaptive serial/parallel |
| F8 | Timer Wheel 集成到 superstep：epoch-based lazy clear，thread-local unified log | Integrated timer wheel |
| F9 | 107 条逻辑链路适配性验证：经典游戏 30 条 + 真实业务 77 条，0% 无法适配 | Empirical validation |
| F10 | 适配分类指导手册：6 大底层原理分类，5 步判定流程 | Adaptation taxonomy |

---

## Paper 1: Cordeiro, Goldman, Da Silva — "Load Balancing on an Interactive Multiplayer Game Server" (Euro-Par 2007)

### 1. 论文核心内容概述

本文以 QuakeWorld 服务器为实验对象，为其 frame 处理流程建立了 BSP（Bulk Synchronous Parallel）并行化模型。每帧被建模为一个 BSP superstep：

1. **输入/调度阶段**：接收客户端输入，确定本帧需要处理的实体集合
2. **本地并行计算阶段**：多线程并行处理各自分配到的实体（移动、碰撞检测、射击判定等）
3. **全局通信和同步阶段**：一个全局 barrier，所有线程同步后进行状态合并和网络广播

核心贡献是负载均衡策略：通过监测每个实体在前几帧的处理耗时，动态重新分配实体到不同线程，将多处理器利用率从约 40% 提高到约 55%。每帧只有 **一个** barrier 同步点。

论文的并行化粒度是 per-entity：每个玩家/NPC 实体是最小的可调度单元，分配到线程后独立执行。锁用于保护共享的碰撞结构（area nodes）和实体状态。

### 2. 与目标系统的重叠点

| 维度 | 重叠描述 | 深度 |
|------|---------|------|
| BSP 框架 | 每帧建模为 BSP superstep，计算 → barrier → 同步 | 表层概念一致 |
| Per-entity 并行 | 实体作为可调度单元 | 类似 Logic=Owner 的调度单元 |
| 负载均衡 | 基于历史耗时动态分配 | 类似 LPT 负载均衡思路 |
| 游戏服务器并行化 | 同为 MMORPG/FPS 服务器场景 | 应用域一致 |

### 3. 与目标系统的差异点

| 目标系统特征 | 差异 |
|-------------|------|
| F1 多轮 superstep | Cordeiro 每帧仅 1 个 barrier，不支持多轮 superstep 循环 |
| F2 Think/Apply 两阶段 | 无读写分离概念，计算阶段直接读写共享状态，依赖锁保护 |
| F3 Ownership 模型 | 无显式 owner 概念，实体可被任意线程的碰撞检测修改 |
| F4 Typed Effect | 无 effect 抽象，副作用通过直接写共享状态 + 锁实现 |
| F5 Signal 双缓冲 | 无 signal 概念，无双缓冲机制 |
| F6 Block-based Collector | 无 cache-aware 的 effect 收集机制 |
| F7 自动串/并行切换 | 无自适应模式切换 |
| F8 Timer Wheel | 无 timer 集成 |
| F9 适配性验证 | 仅在 Quake 单个游戏上测试，无系统性适配分析 |
| F10 适配分类 | 无适配指导方法论 |

### 4. 威胁等级：2 / 5

### 5. 关键判断

**不构成实质性威胁。** Cordeiro 的工作证明了 BSP 模型可以用于游戏服务器帧处理——这是共享的启发来源（BSP 用于游戏），但其设计停留在"把实体分到不同线程 + 一个 barrier"的粗粒度层面。没有读写分离、没有 typed effect、没有 ownership 抽象、没有多轮 superstep。目标系统的核心贡献（ownership 模型、typed effect 代数性质、多轮 superstep 循环、适配性验证）在本文中完全不存在。

本文适合作为 **Related Work 中 BSP 在游戏领域应用的早期探索** 来引用。

---

## Paper 2: Abdelkhalek, Bilas — "Parallelization and Performance of Interactive Multiplayer Game Servers" (IPDPS 2004)

### 1. 论文核心内容概述

本文是最早系统研究 Quake 服务器多线程并行化的工作之一。作者实现了一个共享内存多线程版本的 Quake 服务器，并深入分析了性能瓶颈：

1. **任务分解**：按玩家将帧处理划分为独立任务
2. **同步机制**：使用 pthread mutex 锁保护共享数据结构（BSP tree、area nodes、entity state）
3. **性能发现**：
   - 锁同步开销占总执行时间约 **35%**
   - 全局同步点的等待时间占约 **40%**（由细粒度负载不均衡导致）
   - 并行版本仅比单线程多支持约 **25%** 的玩家数
4. **结论**：纯粹的锁 based 并行化在交互式游戏服务器中效率低下，需要利用游戏特定知识来优化

论文的核心价值是 **诊断性的**——它揭示了为什么朴素的多线程+锁方案在游戏服务器中表现不佳，为后续工作（包括 Atomic Quake、QuakeTM、SynQuake）提供了出发点。

### 2. 与目标系统的重叠点

| 维度 | 重叠描述 | 深度 |
|------|---------|------|
| 问题域 | 同为游戏服务器多核并行化 | 应用域一致 |
| Per-player 任务分解 | 按玩家划分工作单元 | 类似 Logic=Owner 的调度思路 |
| 全局 barrier | 帧结束时全局同步 | BSP 式 barrier |
| 瓶颈分析 | 发现锁开销和同步等待是主要瓶颈 | 问题诊断一致 |

### 3. 与目标系统的差异点

| 目标系统特征 | 差异 |
|-------------|------|
| F1-F10 全部 | 本文是纯诊断/测量工作，不提出新的执行模型或抽象 |
| 核心方法论 | 使用 pthread 锁保护共享状态，这正是目标系统通过 ownership + typed effect 完全避免的 |
| 设计哲学 | 本文尝试"最小修改并行化"，目标系统是"从框架层面重新设计并发模型" |

### 4. 威胁等级：1 / 5

### 5. 关键判断

**不构成任何威胁。** 本文是问题发现型工作，不是解决方案型工作。它揭示的瓶颈（锁开销 35%、同步等待 40%）恰好是目标系统通过 ownership + read-only snapshot + typed effect 所针对解决的问题。可以作为 **motivation 引用**——"Abdelkhalek 2004 证明了朴素锁方案的失败，本文提出的 ownership 模型从根本上消除了这些锁"。

---

## Paper 3: Zyulkyarov et al. — "Atomic Quake: Using Transactional Memory in an Interactive Multiplayer Game Server" (PPoPP 2009)

### 1. 论文核心内容概述

Atomic Quake 是第一个在大型复杂应用中全面使用 Transactional Memory（TM）替代锁进行同步的尝试。作者从 Abdelkhalek 的并行锁版 Quake 服务器出发，将所有锁 based 临界区替换为事务块（atomic blocks）。

关键发现：

1. **事务特征极端**：事务长度从 200 cycles 到 1.3M cycles 不等；读写集从几字节到 1.5MB；嵌套事务最深 9 层
2. **I/O 在事务中**：游戏逻辑在事务中执行系统调用和 I/O 操作，这对 TM 系统提出了非标准需求
3. **编程性权衡**：某些场景下事务简化了程序结构（不需要手动管理锁顺序），某些场景下反而模糊了代码结构
4. **性能**：依赖具体 TM 实现，早期 STM 性能较差

本文的定位是 **TM 可编程性研究**，而非提出新的游戏并行化架构。

### 2. 与目标系统的重叠点

| 维度 | 重叠描述 | 深度 |
|------|---------|------|
| 问题域 | 同为解决游戏服务器共享状态并发访问问题 | 应用域一致 |
| 消除显式锁 | TM 和 typed effect 都旨在消除手动锁管理 | 目标一致但方法完全不同 |
| 并行帧处理 | 帧内多线程并行处理实体 | 表层结构类似 |

### 3. 与目标系统的差异点

| 目标系统特征 | 差异 |
|-------------|------|
| F2 Think/Apply | TM 没有读写分离阶段，事务中可同时读写任意共享状态 |
| F3 Ownership | TM 无 owner 概念，任何线程可事务性地修改任意状态 |
| F4 Typed Effect | TM 使用 opaque 事务（closure-like），非 typed effect |
| F4 Commutativity | TM 冲突检测是 read-write set based，非代数性质 |
| F1 Multi-round | 无多轮 superstep 概念 |
| F5-F8 | 无 signal 双缓冲、block collector、自适应切换、timer 集成 |
| F9-F10 | 无适配性验证或分类方法论 |
| 并发模型 | TM 是乐观并发（先执行后冲突检测/回滚），目标系统是结构性避免冲突（ownership 保证无冲突） |
| 确定性 | TM 回滚导致非确定性执行，目标系统通过 commutativity 保证确定性聚合 |

### 4. 威胁等级：1.5 / 5

### 5. 关键判断

**不构成实质性威胁。** Atomic Quake 走的是完全不同的技术路线——用 TM 替代锁，本质上仍是"允许任意共享状态的并发修改，通过冲突检测保证一致性"。目标系统走的是"通过 ownership 在结构上消除冲突可能性"的路线。两者解决同一个问题，但方法论完全正交。

Atomic Quake 的价值在于证明了 **TM 路线在游戏服务器中面临的挑战**（巨大事务、I/O、嵌套），这可以作为 Related Work 中解释"为什么我们不选择 TM 路线"的论据。

---

## Paper 4: Gajinov et al. — "QuakeTM: Parallelizing a Complex Sequential Application Using Transactional Memory" (ICS 2009)

### 1. 论文核心内容概述

QuakeTM 与 Atomic Quake 来自同一研究组（Barcelona Supercomputing Center），但采取了不同的出发点：

- **Atomic Quake**：从已有的并行锁版本出发，替换锁为事务
- **QuakeTM**：从**顺序版本**出发，使用 task-parallel + STM 直接并行化

关键设计：

1. **任务划分**：将帧处理分解为可并行执行的任务（per-entity game logic）
2. **STM 同步**：每个任务内的共享数据访问被包裹在事务中
3. **粗粒度事务**：由于游戏逻辑的交互复杂性，事务粒度较粗

关键发现：

1. **高开销**：STM 的 instrumentation overhead 在细粒度数据访问模式下非常高
2. **高 abort rate**：由于事务粗粒度和实体间频繁交互（碰撞检测、射线追踪），事务冲突率高
3. **可扩展性差**：随着线程数增加，abort rate 进一步恶化
4. **编程体验**：从顺序代码出发使用 TM 确实简化了并行化过程（相比锁方案）

### 2. 与目标系统的重叠点

| 维度 | 重叠描述 | 深度 |
|------|---------|------|
| Task-parallel | 按实体划分可并行任务 | 类似 per-owner 调度 |
| 从顺序出发 | 寻求将串行游戏逻辑并行化的方法论 | 目标一致 |
| 问题域 | 同为游戏服务器并行化 | 应用域一致 |

### 3. 与目标系统的差异点

| 目标系统特征 | 差异 |
|-------------|------|
| F2 Think/Apply | 无读写分离，事务中直接读写共享状态 |
| F3 Ownership | 无 owner 概念，依赖 TM 冲突检测 |
| F4 Typed Effect | 无 typed effect，使用 opaque 事务 |
| F1 Multi-round BSP | 无 superstep 概念 |
| 冲突处理 | TM: 检测冲突 → 回滚重试。目标系统: 通过结构设计消除冲突 |
| 性能模型 | TM 有高 abort overhead。目标系统无 abort（ownership 保证无冲突） |
| F5-F10 | 全部缺失 |

### 4. 威胁等级：1.5 / 5

### 5. 关键判断

**不构成实质性威胁。** QuakeTM 代表了 TM 路线的另一个变体，与 Atomic Quake 类似，核心方法论与目标系统完全不同。其价值在于证明了"粗粒度 TM 在游戏服务器中性能不佳"，进一步支持目标系统"通过结构设计避免冲突"这一更优路线。

---

## Paper 5: Lupei et al. — "Transactional Memory Support for Scalable and Transparent Parallelization of Multiplayer Games" (EuroSys 2010, SynQuake)

### 1. 论文核心内容概述

SynQuake 是这一系列 Quake 并行化工作中设计最成熟的一篇。作者创建了 SynQuake benchmark——一个 2D 版 Quake 3，保留了 Quake 3 的核心数据结构和服务器帧结构，但可以使用合成工作负载驱动，从而进行可控实验。

核心设计：

1. **Stage-based parallelism**：将帧处理划分为多个阶段（stages），每个阶段内并行，阶段间有 barrier
2. **Interest-based partitioning**：基于空间局部性（area nodes）划分工作，减少跨分区交互
3. **STM 用于跨分区冲突**：当实体处理跨越空间分区边界时，使用 STM 保护
4. **与锁方案的对比**：系统性地比较了 lock-based、TM-based 和 stage-based 三种策略

关键结论：

1. STM 在游戏服务器中可以提供比锁更好的性能（特别是在 stage-based 结构下）
2. 将 Atomic Quake / QuakeTM 归类为"侧重可编程性但性能差"的路径
3. Stage-based + TM 组合在 8 核上实现了接近线性的加速比

论文将帧处理分为多个阶段，这在结构上与 Think/Apply 两阶段有表面相似性，但阶段划分的目的和语义完全不同。

### 2. 与目标系统的重叠点

| 维度 | 重叠描述 | 深度 |
|------|---------|------|
| Stage-based + barrier | 帧内多阶段 + barrier 同步 | 表面结构相似 |
| 空间分区 | 基于空间局部性减少交互 | 类似 block-based 思路 |
| 性能优化焦点 | 关注减少同步开销 | 目标一致 |
| 与 TM 路线对比 | 系统性比较不同并行策略 | 方法论参考价值 |
| Benchmark 构建 | 构建可控 benchmark 进行评估 | 类似 F9 的验证方法 |

### 3. 与目标系统的差异点

| 目标系统特征 | 差异 |
|-------------|------|
| F2 Think/Apply 语义 | SynQuake 的 stage 是功能性划分（movement → collision → shooting），非读写分离 |
| F3 Ownership | 无 owner 概念。阶段内仍依赖 STM 处理跨分区冲突 |
| F4 Typed Effect | 无 typed effect 抽象，副作用通过直接修改共享状态（事务保护） |
| F4 Commutativity | 无代数性质要求 |
| F1 Multi-round | 每帧固定阶段序列，不是 superstep 循环 |
| F5 Signal 双缓冲 | 无 signal 概念 |
| F6 Block Collector | 空间分区是为减少 TM 冲突，非 cache-aware effect 收集 |
| F7 自适应切换 | 无串行/并行自适应 |
| F8 Timer Wheel | 无 |
| F9 适配性验证 | 仅在 Quake 变体上测试，不是跨游戏类型验证 |
| F10 适配分类 | 无适配方法论 |
| 核心差异 | SynQuake 仍依赖 TM 作为冲突解决后盾；目标系统通过 ownership 在结构上消除冲突 |

### 4. 威胁等级：2.5 / 5

### 5. 关键判断

**SynQuake 是所有论文中与目标系统重叠最多的一篇，但仍不构成实质性威胁。** 重叠在于"stage-based + barrier"的结构模式和"空间分区减少交互"的优化思路。但核心差异是根本性的：

- SynQuake 的 stage 是**功能性管线**（movement → collision → shooting），目标系统的 Think/Apply 是**读写分离模型**
- SynQuake 仍然需要 STM 作为跨分区冲突的后盾，目标系统通过 ownership 完全消除冲突
- SynQuake 没有 typed effect、commutativity、signal 双缓冲等核心抽象
- SynQuake 没有适配性验证方法论

SynQuake 应在 Related Work 中重点讨论，解释"stage-based parallelism 的演进路径"以及"为什么 ownership 模型优于 TM-based 冲突解决"。

---

## Paper 6: Mohebali, Chiew — "Redefining Game Engine Architecture Through Concurrency" (SoMeT 2014)

### 1. 论文核心内容概述

本文提出将传统线性（sequential）游戏引擎架构重新设计为并发友好的架构。核心设计：

1. **中央调度器**：一个主调度器管理所有游戏引擎模块的执行
2. **主时钟 tick**：固定频率的主循环 tick 驱动所有模块
3. **模块级并行**：每 tick 将不同引擎模块（渲染、物理、AI、音频等）的任务提交给线程池
4. **同步等待**：每 tick 结束前，调度器等待所有模块任务完成后再进入下一 tick
5. **模块依赖分析**：识别主要引擎模块间的依赖关系，据此确定哪些模块可以并行

实验结果显示，并发架构在简单测试游戏中比线性架构提升 5.1% 到 61.2% 性能。

### 2. 与目标系统的重叠点

| 维度 | 重叠描述 | 深度 |
|------|---------|------|
| Tick-driven | 固定频率主循环 | 基本架构相似 |
| 中央调度器 | 调度器管理并行执行 | 表层概念一致 |
| Barrier 同步 | 每 tick 结束全局同步 | BSP-like |
| 模块化 | 识别独立可并行模块 | 概念层面 |

### 3. 与目标系统的差异点

| 目标系统特征 | 差异 |
|-------------|------|
| 并行粒度 | Mohebali 是**模块级**并行（渲染 vs 物理 vs AI），目标系统是**entity/logic 级**并行 |
| 应用场景 | 客户端游戏引擎，非服务器 | 
| F1 Multi-round BSP | 每 tick 仅一个"提交任务 → 等待完成"周期 |
| F2 Think/Apply | 无读写分离概念 |
| F3 Ownership | 无 owner 模型，模块间依赖通过预定义依赖图管理 |
| F4 Typed Effect | 无 effect 抽象 |
| F5-F10 | 全部缺失 |
| 深度 | 学术贡献较浅，仅 2 次引用，属于短论文级别 |

### 4. 威胁等级：1 / 5

### 5. 关键判断

**不构成任何威胁。** 本文的并行化层次（引擎模块级）与目标系统（entity/logic 级）完全不同。目标系统解决的是"同一个物理/游戏逻辑阶段内，如何并行处理数千个实体的 tick 逻辑"，而 Mohebali 解决的是"如何让渲染和物理引擎模块并行运行"。两者是并行化层次谱系中完全不同的位置。

可在 Related Work 中简要提及，归入"模块级并行"类别以区分。

---

## Paper 7: Zamith et al. — "Exploring Parallel Game Architectures With Tardiness Policy" (SBGames 2015)

### 1. 论文核心内容概述

本文提出一个带 tardiness policy（迟到策略）的并行自适应游戏架构：

1. **离散时间步仿真**：将游戏视为 discrete time-stepped simulation
2. **任务并行**：帧内任务被划分为可并行执行的单元
3. **Tardiness 监测**：监控每个任务是否在预定时间步内完成
4. **自适应质量调节**：
   - 在强力硬件上：如果有剩余时间，提升仿真质量（更精确的物理、更好的 AI）
   - 在弱硬件上：降级任务功能以确保实时性
5. **Barrier 同步**：任务间默认同步对象是 barrier，确保时间步一致性

核心创新在于 tardiness policy——一个运行时监测和自适应调节机制，允许游戏在不同硬件上自动调整仿真精度。

### 2. 与目标系统的重叠点

| 维度 | 重叠描述 | 深度 |
|------|---------|------|
| Discrete time-step | 游戏作为离散时间步仿真 | 基本模型一致 |
| Barrier 同步 | 任务间使用 barrier | BSP-like |
| 自适应 | 运行时自适应行为调节 | 类似 F7 的自适应概念 |
| 任务并行 | 帧内任务分解并行执行 | 表层一致 |

### 3. 与目标系统的差异点

| 目标系统特征 | 差异 |
|-------------|------|
| 自适应语义 | Zamith 的自适应是**质量调节**（降低仿真精度），目标系统的自适应是**执行模式切换**（并行↔串行） |
| F2 Think/Apply | 无读写分离 |
| F3 Ownership | 无 owner 模型 |
| F4 Typed Effect | 无 effect 抽象 |
| F1 Multi-round | 无多轮 superstep |
| F5-F6 | 无 signal/effect 收集机制 |
| F8-F10 | 无 timer、适配性验证、分类方法论 |
| 应用场景 | 主要面向客户端渲染和仿真，非服务器 |
| 并行粒度 | 任务级并行，但任务划分不基于 ownership |
| 学术影响 | 仅 2 次引用，发表在区域会议（SBGames） |

### 4. 威胁等级：1 / 5

### 5. 关键判断

**不构成任何威胁。** Zamith 的 tardiness policy 是一个有趣的运行时自适应概念，但其自适应的维度（仿真质量 vs 执行模式）与目标系统完全不同。目标系统的"串/并行自适应切换"解决的是调度效率问题（工作量不足时串行更快），而 tardiness policy 解决的是实时性保障问题（来不及就降级）。两者虽然都涉及"自适应"，但属于不同的设计空间。

---

## 综合结论

### 已被先行工作覆盖的概念（非新颖）

以下概念在先行工作中已有出现，目标系统不能声称首创：

| 概念 | 覆盖来源 | 说明 |
|------|---------|------|
| BSP 模型用于游戏帧处理 | Paper 1 (Cordeiro 2007) | BSP superstep + barrier 用于游戏服务器，概念级已建立 |
| Per-entity 并行任务划分 | Paper 1, 2 | 将实体作为可并行调度的最小单元 |
| Stage-based + barrier 帧结构 | Paper 5 (SynQuake 2010) | 帧内多阶段 + barrier 同步 |
| 负载均衡策略 | Paper 1 (Cordeiro 2007) | 基于历史耗时的动态负载均衡 |
| 离散时间步 + barrier 同步 | Paper 7 (Zamith 2015) | 游戏作为 discrete time-stepped simulation with barriers |

### 目标系统的新颖贡献（先行工作中未出现）

| 贡献 | 新颖性说明 | 对应特征 |
|------|----------|---------|
| **Logic=Owner 所有权模型** | 所有先行工作依赖锁或 TM 处理共享状态冲突；无一提出将写权限绑定到 owner 来**结构性消除冲突**。这是目标系统最核心的区别点。 | F3 |
| **Think/Apply 读写分离** | 先行工作的 stage 划分是功能性的（movement vs collision）或按 TM 事务包裹；无一实现"Think 只读快照 + Apply 按 owner 聚合"的读写分离模型。 | F2 |
| **Typed Effect + 代数性质（commutativity）** | 所有先行工作的副作用都是直接写共享状态（锁/TM 保护）或 opaque closure；无一提出将 effect 类型化并要求代数性质（可交换性）以支持无序安全聚合。 | F4 |
| **多轮 Superstep 循环** | Cordeiro 每帧 1 个 barrier；SynQuake 固定阶段序列；无一支持同一 tick 内动态多轮 Think→Apply 循环直到收敛。 | F1 |
| **Signal 双缓冲传递** | 完全无先例。先行工作无 signal 概念，更无双缓冲 swap 的无锁传递机制。 | F5 |
| **Block-based Effect Collector** | 完全无先例。per-thread per-block + CacheLinePad + sort-based 分组是目标系统独有的工程贡献。 | F6 |
| **自动串/并行模式切换** | Zamith 的自适应是质量降级；无一提出基于工作量阈值在同一语义框架内切换执行模式（truly inline 串行 vs 并行），且串行是终态。 | F7 |
| **Timer Wheel 集成到 superstep** | 完全无先例。epoch-based lazy clear + thread-local unified log 集成到 superstep 生命周期。 | F8 |
| **107 条逻辑链路适配性验证** | 所有先行工作仅在 Quake 系列（单款 FPS）上测试。目标系统跨 3 款经典游戏 + 真实业务 8 个子系统的 107 条链路验证是前所未有的覆盖广度。 | F9 |
| **适配分类指导手册** | 完全无先例。6 大底层原理分类 + 5 步判定流程 + 改造模式速查，将"如何判断一条逻辑是否可以并行化"形式化为可复用方法论。 | F10 |

### 威胁等级汇总

| Paper | 威胁等级 | 角色定位 |
|-------|---------|---------|
| P1: Cordeiro 2007 | 2/5 | Related Work: BSP 在游戏领域的早期探索 |
| P2: Abdelkhalek 2004 | 1/5 | Motivation: 证明锁方案的失败 |
| P3: Atomic Quake 2009 | 1.5/5 | Related Work: TM 路线的可编程性探索 |
| P4: QuakeTM 2009 | 1.5/5 | Related Work: TM 路线的另一变体 |
| P5: SynQuake 2010 | 2.5/5 | Related Work: 最接近的先行工作（stage-based + barrier） |
| P6: Mohebali 2014 | 1/5 | Related Work: 模块级并行（不同层次） |
| P7: Zamith 2015 | 1/5 | Related Work: tardiness 自适应（不同维度） |

### 总结论

**目标系统的新颖性是充分的。** 7 篇先行工作中：

1. **无一具备 ownership 模型**——这是目标系统的核心创新，从根本上改变了"如何处理共享状态并发访问"的方法论（从"检测冲突"转向"结构性消除冲突"）
2. **无一具备 typed effect + commutativity**——这是保证 Apply 阶段无序安全的关键抽象
3. **无一具备多轮 superstep + signal 双缓冲 + 自适应切换**——这三者构成了完整的执行模型创新
4. **无一进行跨游戏类型的适配性验证**——107 条链路覆盖的广度和深度在该领域前所未有
5. **无一产出适配方法论**——6 分类 + 5 步流程将"并行化判定"从特定案例提升为可复用知识

先行工作的最大价值在于提供了 **问题诊断**（Abdelkhalek）、**反面论据**（Atomic Quake / QuakeTM 证明 TM 路线的局限）和 **BSP 概念在游戏领域的先期验证**（Cordeiro / SynQuake）。目标系统应当在 Related Work 中充分引用这些工作，同时清晰地阐述自身在 ownership 模型、typed effect 代数、多轮 superstep 和适配方法论上的独立贡献。

### 建议的 Related Work 叙事结构

```
/dev/null/outline.md#L1-18
1. Game Server Parallelization: The Lock-Based Era
   - Abdelkhalek 2004: 诊断了锁方案的瓶颈（motivation）
   - Cordeiro 2007: BSP 模型的初步应用（概念先驱）

2. The Transactional Memory Detour
   - Atomic Quake 2009: TM 替代锁的首次大规模尝试
   - QuakeTM 2009: 从顺序代码出发的 TM 并行化
   - SynQuake 2010: Stage-based + TM 的综合方案（最接近的先行工作）
   - 结论: TM 路线面临巨大事务/高 abort rate/非确定性执行的根本挑战

3. Game Engine Architecture Approaches
   - Mohebali 2014: 模块级并行（不同粒度层次）
   - Zamith 2015: tardiness 自适应（不同优化维度）

4. Our Approach: Ownership-Based Structural Parallelism
   - 从结构上消除冲突，而非检测/回滚冲突
   - Typed effect + commutativity 保证无序安全
   - Multi-round superstep + signal 双缓冲实现完整执行模型
   - 107 条链路验证 + 适配分类方法论提供工程落地指导
```
