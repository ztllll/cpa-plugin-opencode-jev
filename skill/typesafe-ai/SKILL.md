---
name: typesafe-ai
license: MIT
description: >
  用 TypeSafe 构建带 AI 判断力的软件：像编程原语一样使用小型 AI 智能单元。
  其 System One 模型（包括 Jev）把自然语言和应用状态转化为带类型的判断和
  概率，供代码直接组合使用。当某个功能需要"可编程的常识"、需要把一段
  LLM 提示-解析流程变成结构化决策、或需要路由/分级/抽取/校验/评分时使用。
  本部署通过 CPA 代理访问 Jev（自动轮询 OpenCode Go 账号 key），本地用
  `jev-ask` 命令即可调用。
---

# 用 TypeSafe 构建

TypeSafe 把 AI 智能做成可像编程原语一样使用的单元：小型判断可以组合成更大的
能力。其 **System One 模型**返回快速、聚焦的判断，软件可直接消费。**Jev** 是
旗舰模型：理解自然语言，返回带类型的答案和概率，而不是生成文本或推理过程。
工作流由代码掌控，模型在普通代码需要语义理解的地方提供可编程的常识。

## 本部署的调用方式（先读这节）

调用统一走本机封装（已内置 CPA 地址、管理密钥、key 轮询重试）：

```bash
jev-ask "状态描述" '{
  "is_urgent": {"type":"noul","instructions":"是否紧急？"},
  "department": {"type":"choice","instructions":"哪个组处理？",
                 "criteria":{"billing":"付款问题","returns":"退款退货"}},
  "frustration": {"type":"score","instructions":"愤怒程度？",
                  "criteria":["平静","不满","暴怒"]}
}'
```

- `$1` 状态文本；`$2` 问题 JSON；输出 `{"answers":{...},"model":...,"usage":{...}}`。
- 三种题型：`noul`（是否，返回"是"的概率）、`choice`（**必填** criteria 对象映射选项→含义）、
  `score`（**必填** criteria 档位数组，描述具体情形）。缺 criteria 上游 422。
- 远程机器没有脚本时：`POST $JEV_ENDPOINT/zen/v1/systemone`，
  `Authorization: Bearer $CPA_MANAGEMENT_KEY`，body 为 `{"model":"jev-1.13","state":...,"questions":{...}}`。
  也可用管理路径 `$JEV_ENDPOINT/v0/management/plugins/opencode-jev/ask`（协议等价）。
- key 轮询、401/403/402/429 重试由 CPA 服务端处理，调用方不要传 API key。
- 仅用于判断/分类/评分；生成文本、总结、翻译请用普通模型。

## 读实时文档

**TypeSafe 实时文档是事实来源，做任务时要先读。** 本 skill 给方向；概念、提示词
写法、API 契约、模型与限制以文档为准。

- 先读[文档索引](https://docs.typesafe.ai/llms.txt)找相关页面和 cookbook，按需读取。
- Mintlify 页面加 `.md` 即可取 Markdown，例如
  [如何用 System One 构建](https://docs.typesafe.ai/concepts/how-to-build-with-system-one.md)。
  相对链接按 `https://docs.typesafe.ai` 解析。
- 写集成代码前，读当前 HTTP API 页与相关题型页。若实时访问不可用，使用本地已知
  信息并明确声明，不要臆造版本相关的细节。

| 任务 | 从这里开始 |
| --- | --- |
| 理解编程模型 | [System One](https://docs.typesafe.ai/concepts/system-one.md)、[构建指南](https://docs.typesafe.ai/concepts/how-to-build-with-system-one.md) |
| 探索可构建什么 | [用例地图](https://docs.typesafe.ai/concepts/use-case-map.md) 及索引中的 cookbook |
| 准备输入与问题 | [State](https://docs.typesafe.ai/concepts/state.md)、[primitives](https://docs.typesafe.ai/primitives.md) |
| 处理不确定性 | [Confidence](https://docs.typesafe.ai/confidence.md) |
| 写 API 代码 | [HTTP API](https://docs.typesafe.ai/api.md)（本部署用 jev-ask 代理同一协议） |

## 找到有用的形态

从用户想要的行为出发：应用要展示、选择、修改或分派什么？反推它需要哪些判断。
已知规则、计算、精确查找和执行留在代码里；语义理解的部分才交给 TypeSafe。

- **路由并填充参数**：请求可选中处理器及其类型化参数；提前问清分支专属问题，
  只消费相关答案。见 [function calling](https://docs.typesafe.ai/cookbooks/function_calling.md)、
  [投机扇出](https://docs.typesafe.ai/patterns/fan-out.md)。
- **选择而非生成**：在代码里找候选值/原文片段，用判断选出正确项再复制或归一化。
  见 [值抽取](https://docs.typesafe.ai/cookbooks/pre_parsed_value_extraction_cookbook.md)、
  [结构恢复](https://docs.typesafe.ai/cookbooks/autoformat.md)。
- **检索并判断证据**：召回候选、对相关性打分、选出有用上下文。见
  [重排](https://docs.typesafe.ai/cookbooks/rerank_typesafe.md)、
  [层次分类](https://docs.typesafe.ai/cookbooks/hierarchical_classification.md)。
- **把判断变成可复用数据**：维度打分一次，让代码/用户控件调权重阈值与排序。见
  [复合评分](https://docs.typesafe.ai/patterns/composite-scoring.md)。
- **校验与升级**：对具体断言核对证据，把不确定或失败的 case 交给人工或推理模型。见
  [引用校验](https://docs.typesafe.ai/cookbooks/citation_check.md)。
- **响应状态变化**：代码保留目标与观察，新判断引导下一步有界动作；应用结果前
  先检查新鲜度。

## 设计问题

按答案含义选题型，再读对应 primitive 页：

| 需求 | 题型 | 要点 |
| --- | --- | --- |
| 一组确定选项之一 | [choice](https://docs.typesafe.ai/primitives/choice.md) | 选一个选项；其分布用于比较竞争选项 |
| 条件是否成立 | [noul](https://docs.typesafe.ai/primitives/noul.md) | 是的概率；无单独置信度；多个可能同时成立时每标签问一次 |
| 在描述维度上的程度 | [score](https://docs.typesafe.ai/primitives/score.md) | 有序档位上的概率位置；排序比较用可比的逐项 score |

给每个问题足够的**状态**（原文、身份、关系、政策、当前事实）。上下文有多部分时
优先用命名的 JSON 字段。判断标准写在 **instructions**，候选答案定义在 **criteria**
里；问题 ID 只给代码用、不会发给模型，完整语义要写进问题本身。嵌套状态用反引号
路径引用，如 `ticket.messages[0].text`。

一题一个窄而连贯的判断；独立有用的维度拆开问，但不要拆散被判断的关系本身。
instruction/criteria 需要定义、对比、排除项或示例时，用结构化对象或数组。
score 的档位必须描述具体情境、能独立成立。

确保需要的答案可得：可能都不命中时加"无匹配"选项；需要时单独加一个存在性判断。
做候选值选择时检查候选覆盖：模型选不出没给它的值。

## 组合与校验

**相互独立的问题对同一状态一起问**（含有用的投机问题）——它们并行执行、互相
不可见。需要前一步答案才能取证据/构造新状态时，才发第二次请求。多问题消耗
token，实测预算与延迟。

用概率与置信度引导行为，阈值要在用户自己的数据上评估。choice/score 的
confidence 反映分布集中度，不代表整个流程的正确性或行动许可。noul 接近 0.5
表示"是/否"概率相当，不是强度中等。多个可接受选项会摊薄概率——无害的偏好
选择不必因低置信作废。

保持策略显式、原始判断可复用：加权评分适合可互补的偏好；"任一严重违规"规则
需要独立条件。证据与题意不变时，改权重/展示过滤不必重跑推理。类型化输出只
保证接口，不保证真值；在目标域实测校准。

测代表性用例和最终应用行为。失败时检查确切的状态、问题、候选、答案、组合与
观测结果；区分缺证据、模型错、代码错与服务错。cookbook 的阈值是待评估示例，
不是普适规则。
