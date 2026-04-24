## 背景 & 目标

我们设计了 /sched 做了并发运行的底座(runtime)，现在要在其上实现一个 gas 系统(game ability system)作为框架(framework)，这样用户就可以基于这套 runtime + framework
实现真实的游戏逻辑。

## 思路

在设计 gas 时，我打算借用一些 unreal gas 的基本概念（主要是 attribute set/modifier/game ability/game effects/tag），将一个类似 ASC 的结构体封闭成一个 Logic 提供给我们的 scheduler（并且使用 scheduler 提供的接口访问 world），在实现上我们可以参考一个半成品(/Users/dongcheng/Project/legamerdc/gas, 参考如何将各个可以 think 的组件合并成一个对外 think 的组件)
大概来说 ASC 会被定义为:
```go
// 所有单位类型都会内嵌一个 GAS，并且这些字段都是高度自适应的，特别是 ASC
type GAS[...] struct {
	ASC AttrMap // 核心属性
	Tag Tag // tag 
	
	Abilities ArrayMap // 可以释放的技能
	GameEffects HeapArrayMap // 运行中的技能/buff
	Modifiers ArrayMap // 类似 unreal gas modifiers
	
	// inner map/quick finding/caches
	// ...
}

// 提供给 think 阶段访问其他 logic 的只读属性
type ReadOnlyGAS[...] struct {} 
```
我们需要解决一些问题：
- 我们在 ReadOnlyGAS 中提供哪些内容？这些内容要保证在 think 阶段不会改动，比如 ASC/Tag (相反Abilities/GameEffects/Modifiers在think阶段都需要自我修改)，但这些就足够了吗（在概念上）？我们是否要额外调整 gas 概念来支持接入 scheduler？
- 我们需要实现 unreal gas 中复杂的管线吗？例如 MMC, Aggregator 以及各种回调。我认为不需要，或许我们提供类似半成品(legamerdc/gas)中的接口就够了，把各种实际运行规则留给下一层，这样 GameEffects 就可以完全使用 RunningI 类似的实现，但是 Modifier 呢？我们是否还需要这层抽象？
...

## 要求

- gas/ 的实现需要配合 sched/ 的接口（例如 Think/Apply 等），引用 sched/ 的一些定义(例如 WatchState 等)

## 任务

根据我们的目标和思路自主探索这些仓库，理解设计和问题，然后尝试解决这些问题，并编写一个 gas/ framework第一版实现
