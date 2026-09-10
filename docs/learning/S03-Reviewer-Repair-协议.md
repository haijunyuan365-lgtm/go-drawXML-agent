# Day 3 / S03：Reviewer JSON 协议与独立 Repair 协议

日期：2026-09-09  
代码状态：已完成  
验证状态：离线测试通过；未做真实模型调用  
理解状态：待复述

## 1. 本日问题与原行为

原绘图入口并不是“Reviewer 只给意见”。真实链路是：

```text
用户请求
  -> agent_analyst：输出 analysis_result
  -> agent_drawer：输出 draft_diagram XML
  -> agent_reviewer：检查 XML、引用和布局，并直接修图
  -> agent_reviewer：输出 final_result 裸 XML
  -> Day 2 drawio-xml Validator
  -> Runner / HTTP 返回 XML
```

例如用户要求“下单成功后扣减库存”，初稿可能有“订单服务”和“库存服务”两个节点，却漏掉调用边。原 Reviewer 在一次模型调用中既判断缺边，又直接改 XML。运行时只得到最终字符串，因此程序不知道：

- 原图是否一次通过；
- Reviewer 发现了什么问题；
- Reviewer 修改了什么；
- 后续应该重试、修图还是因协议错误退出。

Day 2 已经解决了“最终字符串是不是本项目支持的 Draw.io XML”这个确定性问题，但它不能判断“需求中的库存扣减关系是否画出来”。Day 3 要建立下一层协议，同时保持现有入口不变。

## 2. 目标行为与整体数据流

本日把原 Reviewer 的两种职责拆成两个可独立测试的协议：

```text
原始需求 + 分析结果 + 当前 XML
  -> Reviewer Prompt
  -> 模型 JSON 字符串
  -> 严格 ParseResponse
       -> 合法且 passed=true：语义通过
       -> 合法且 passed=false：普通质量不通过，issues 可交给 Repair
       -> JSON/字段/一致性错误：review_protocol_error

原始需求 + 分析结果 + 当前 XML + 本轮 issues
  -> Repair Prompt
  -> 模型完整 XML 字符串
  -> Repair ParseResponse
       -> 裸 XML 且 Day 2 Validator 通过：得到原样候选 XML
       -> 非裸 XML 或结构错误：validation.Error
```

当前生产链路仍保持：

```text
Analyst -> Drawer -> 原 agent_reviewer（检查 + 直接修图，返回 XML）
        -> drawio-xml Validator -> 对外返回 XML
```

新的 Reviewer JSON 与 Repair 协议尚未接入 Runner。Day 4 才会增加请求级状态和有限修复循环，Day 5 再切换真实绘图入口。这样不会出现过渡版本把 Reviewer JSON 当作图纸加载到画布。

## 3. 固定的数据契约

### 3.1 Reviewer 输入

```json
{
  "original_requirement": "用户最初的绘图要求",
  "analysis_result": "Analyst 整理后的需求",
  "current_xml": "<mxfile>...</mxfile>"
}
```

三个字段都必填。原始需求不能省略，否则 Reviewer 只能验证 Analyst 的二手解释，无法发现分析阶段漏掉的硬约束。

### 3.2 Reviewer 输出

通过：

```json
{
  "passed": true,
  "issues": []
}
```

不通过：

```json
{
  "passed": false,
  "issues": [
    {
      "type": "missing_edge",
      "description": "缺少订单服务到库存服务的调用关系",
      "element_ids": ["order", "inventory"]
    }
  ]
}
```

第一版所有 issue 都是阻断问题，因此固定不变量是：

```text
passed == (len(issues) == 0)
```

`passed=false` 是一次成功执行的审查结论，不是程序错误。空回复、截断 JSON、Markdown 代码块、未知字段、字段类型错误、必填字段缺失以及 `passed/issues` 矛盾，才是 `review_protocol_error`。

`type` 目前要求非空，但不冻结为枚举，避免 Day 3 为所有未来语义问题预设错误分类。`element_ids` 必须存在；问题无法映射到已有元素时使用空数组。

### 3.3 Repair 输入

```json
{
  "original_requirement": "用户最初的绘图要求",
  "analysis_result": "Analyst 整理后的需求",
  "current_xml": "<mxfile>当前版本</mxfile>",
  "issues": [
    {
      "source": "reviewer",
      "code": "missing_edge",
      "message": "缺少订单服务到库存服务的调用关系",
      "element_ids": ["order", "inventory"]
    }
  ]
}
```

`source` 只有 `validator` 和 `reviewer` 两种。这样 `dangling_reference` 仍表示 Validator 发现“引用了不存在的 id”，`missing_edge` 仍表示 Reviewer 发现“需求里的关系没画”，两者不会因都与“边”有关而混为一类。

`current_xml` 明确表示当前候选，而不是固定的初稿。Day 4 每次修复成功后必须更新它，下一轮只能修最新版本。

### 3.4 Repair 输出

Repair 只返回完整、单页、未压缩的裸 `mxfile` XML：

- 第一个字符必须属于 `<mxfile`；
- 最后一个字符必须结束于 `</mxfile>`；
- 不允许前后空白、Markdown、JSON、解释文字；
- 通过 Day 2 `drawio.Validator` 后，逐字返回原 XML，不做静默清洗。

Repair 的坏 XML 归入 `validation.Error`，而不是 `review_protocol_error`。这项区分是为 Day 4 做准备：控制器可以把一次坏修复计入修复预算，再根据剩余预算决定是否继续；Reviewer 的 JSON 协议损坏则直接退出，不能猜测审查结论。

## 4. 改动总表

| 文件 | 类型 | 改前职责 | 改后职责 | 原因 |
|---|---|---|---|---|
| `internal/domain/diagram/reviewer/protocol.go` | 新增 | 无独立语义审查协议 | 定义 Reviewer 提示、输入/输出、严格 JSON Parser 和 `ProtocolError` | 让程序可靠区分通过、质量不通过、协议错误 |
| `internal/domain/diagram/reviewer/protocol_test.go` | 新增 | 无协议级测试 | 覆盖正常结果、截断/类型/字段/矛盾等异常回复 | 防止“提示词说 JSON”被误当成可靠结构化输出 |
| `internal/domain/diagram/repair/protocol.go` | 新增 | 修图职责藏在原 Reviewer | 定义独立 Repair 输入、问题适配、提示和 XML 输出校验 | 让 Repair 明确修当前版本的本轮问题 |
| `internal/domain/diagram/repair/protocol_test.go` | 新增 | 无独立 Repair 测试 | 覆盖问题来源、当前候选、裸 XML 与坏 XML | 固定后续循环的数据边界 |
| `internal/app/config/loader_test.go` | 修改 | 只确认 `drawio-xml` 已启用 | 另确认生产工作流仍以原 Reviewer 裸 XML 收尾 | 防止 Day 3 过渡期把 JSON 送进画布 |
| `docs/learning/S03-Reviewer-Repair-协议.md` | 新增 | 无 Day 3 学习材料 | 记录协议、逐块实现、验证、限制和练习 | 满足每日学习与交接标准 |
| `AGENT_EVOLUTION_PLAN.md` | 修改 | S03 未开始 | 更新 S03 状态、验证事实与 Day 4 入口 | 保持唯一进度入口准确 |

本日没有修改 `configs/agent/agent-draw-io.yaml`、Runner、HTTP Handler、前端或模型客户端。

## 5. 逐文件、逐逻辑代码块讲解

### 5.1 Reviewer：角色提示和输入

位置：[Reviewer Instruction 与 Input](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/domain/diagram/reviewer/protocol.go:29)

`Instruction` 保留了原 Reviewer 的核心检查思想：核心节点、关系方向、标签、明显重叠与连线问题；同时把职责收窄为“只读审查”。确定性 XML 语法、ID 和引用仍由 Day 2 Validator 检查，Reviewer 不能修改 XML，也不能声称已经实际渲染图纸。

`Input` 同时保存原始需求、分析结果和当前 XML。它属于 diagram 领域协议，不依赖 HTTP DTO 或具体模型 SDK，因此离线测试和未来工作流控制器可以复用。

### 5.2 Reviewer：`BuildPrompt` 与负载编码

位置：[Reviewer BuildPrompt](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/domain/diagram/reviewer/protocol.go:90)

执行顺序：

1. 检查原始需求非空；
2. 检查分析结果非空；
3. 检查当前 XML 非空；
4. 把三者编码为一个 JSON payload；
5. 返回“可信角色指令 + 运行数据”两个独立字符串。

运行数据不用手写 `---` 分隔符拼接，因为用户文本或 XML 可能碰巧包含分隔符。JSON 编码保留字符串边界。编码器关闭 HTML 转义，让 `<mxfile>` 保持模型可读形式，不变成大量 `\u003c` / `\u003e`；JSON 自身仍负责引号、换行等转义。

### 5.3 Reviewer：公开结果与内部 wire 类型

位置：[Issue、Result 与 ProtocolError](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/domain/diagram/reviewer/protocol.go:52)

`Result` 是业务代码可以信任的结果。`wireResult` / `wireIssue` 是 Parser 内部临时结构，字段使用指针，原因是 Go 的零值无法区分：

- JSON 明确给出 `"passed": false`；
- JSON 完全漏掉 `passed`。

同样，`issues` 和 `element_ids` 的指针可以拦住字段缺失或 `null`。严格协议不把缺失数组默认为空数组。

`ProtocolError` 对外提供稳定 `code=review_protocol_error`，`violation` 再细分 `empty_response`、`invalid_json`、`trailing_content`、`missing_passed`、`missing_issues`、`invalid_issue` 和 `inconsistent_result`。它不保存或输出原始模型响应，避免错误对象无界增大或意外暴露完整图纸。

### 5.4 Reviewer：`ParseResponse`

位置：[Reviewer ParseResponse](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/domain/diagram/reviewer/protocol.go:133)

Parser 按以下顺序执行：

1. 空白回复立即归类为 `empty_response`；
2. `json.Decoder` 解析一个对象，并用 `DisallowUnknownFields` 拒绝额外字段；
3. 再解码一次，只有 `io.EOF` 才说明不存在第二个 JSON 或尾随文字；
4. 检查 `passed` 和 `issues` 是否显式存在；
5. 逐个检查 issue 的三个字段、非空内容和元素 id；
6. 最后验证 `passed == (issues 为空)`。

顺序很重要。例如 `{"passed": false, "issues": []}` 虽然 JSON 语法正确，但没有任何可交给 Repair 的问题，属于矛盾协议，而不是普通不通过。

`passed=false` 且至少一个合法 issue 会返回 `Result, nil`。Day 4 控制器将据此进入 Repair；Parser 本身不做循环，也不追加模型调用。

### 5.5 Repair：提示、输入与统一问题结构

位置：[Repair Instruction 与 Input](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/domain/diagram/repair/protocol.go:19)

Repair 提示要求“只修列出的问题、保留正确内容、返回完整 XML”。这和原 Reviewer 的“一边检查一边直接改”不同：Repair 不负责决定图是否合格，只负责按明确问题转换当前候选。

`Issue` 将两种来源统一成 Repair 可消费的字段，同时保留 `source`：

- Validator 问题使用已有 `code/message/path/attribute/elementId`；
- Reviewer 问题把 `type/description/element_ids` 映射成 `code/message/element_ids`。

### 5.6 Repair：问题适配函数

位置：[FromValidationIssues](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/domain/diagram/repair/protocol.go:103) 与 [FromReviewerIssues](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/domain/diagram/repair/protocol.go:124)

`FromValidationIssues` 保留确定性定位信息，单个 `ElementID` 转成最多一个元素的数组。`FromReviewerIssues` 复制元素 id slice，避免调用方后续修改原 slice 时悄悄改变 Repair 输入。

适配函数不合并或改名问题码，所以 `dangling_reference` 与 `missing_edge` 仍有不同含义，后续 Eval 可以按来源统计。

### 5.7 Repair：`BuildPrompt`

位置：[Repair BuildPrompt](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/domain/diagram/repair/protocol.go:59)

它要求原始需求、分析结果、当前 XML 都非空，且至少有一个问题。每个问题必须来自 Validator 或 Reviewer，并包含非空 `code/message`；元素 id 如果存在也必须非空。

这里故意不要求 `CurrentXML` 先通过 Validator。原因是 Validator 发现结构问题时，Repair 正是要接收一份坏 XML 和对应问题。提前拒绝坏 XML 会让结构修复路径永远无法发生。

### 5.8 Repair：`ParseResponse` 与 Day 2 Validator 复用

位置：[Repair ParseResponse](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/domain/diagram/repair/protocol.go:140)

`validateBareCandidate` 先检查输出边界：不能有前后空白或解释文字，必须从 `<mxfile` 开始并以 `</mxfile>` 结束。随后调用已有 `drawio.NewValidator().Validate`，继续检查单页未压缩结构、ID、引用和 geometry。

校验通过时返回原字符串，不自动 `TrimSpace`、提取代码块或修正 XML。静默清洗会掩盖模型没有遵守协议，也会让实际评测结果失真。

校验失败时返回 `validation.Error`，Validator 名为 `repair-drawio-xml`，并保留结构化 issues。S04 可以直接把这些问题作为下一次 Repair 输入。

### 5.9 Reviewer 测试

位置：[Reviewer 协议测试](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/domain/diagram/reviewer/protocol_test.go:11)

- `TestBuildPromptCarriesCompleteReviewContext`：验证三个输入完整传递，XML 标签没有被 HTML 转义；
- `TestParseResponseDistinguishesPassAndQualityRejection`：固定成功与质量不通过都不是协议错误；
- `TestParseResponseRejectsMalformedOrContradictoryReplies`：表格覆盖空回复、截断、代码块、缺字段、错类型、`null`、未知字段、坏 issue、矛盾结果和尾随对象；
- `TestParseResponseNormalizesIssueWhitespace`：固定下游收到经过边界空白归一化的问题内容。

### 5.10 Repair 测试

位置：[Repair 协议测试](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/domain/diagram/repair/protocol_test.go:16)

- `TestBuildPromptCarriesCurrentCandidateAndIssues`：证明传的是当前候选与本轮问题；
- `TestIssueAdaptersKeepValidatorAndReviewerMeaningSeparate`：防止两个“边问题”被错误合并；
- `TestParseResponseReturnsExactValidatedXML`：合法 XML 逐字返回；
- `TestParseResponseRejectsNonBareOrInvalidXML`：覆盖 Markdown、空白、解释文字和结构缺失；
- `TestBuildPromptRejectsMissingOrUnusableIssueData`：没有问题或未知来源不能启动 Repair。

### 5.11 生产入口回归测试

位置：[配置边界回归测试](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/app/config/loader_test.go:22)

测试读取真实 `agent-draw-io.yaml`，固定以下事实：

1. Runner 仍选择 `sequential_draw_process`；
2. 子 Agent 顺序仍是 `agent_analyst -> agent_drawer -> agent_reviewer`；
3. 原 `agent_reviewer` 仍使用 `final_result`，提示明确禁止 JSON 并要求 `</mxfile>`；
4. 最终仍启用 `drawio-xml` Validator。

这不是通过 mock 猜测接口行为，而是直接检查当前生产 YAML 的真实装配输入。

## 6. 关键代码 Before / After

### 6.1 Reviewer 职责

Before：

```text
Reviewer(XML):
  检查问题
  直接修改
  return 完整 XML
```

After（Day 3 新协议，尚未接入生产入口）：

```text
Reviewer(requirement, analysis, currentXML):
  return 严格 JSON Result

Repair(requirement, analysis, currentXML, issues):
  return 完整 XML
```

原 Reviewer 暂时保留，避免 Day 3 半接入。S05 完整闭环接好后，生产入口才一次切换。

### 6.2 JSON 处理

Before：

```go
// 没有 Reviewer JSON；Runner 只接收一个 string。
```

After（简化）：

```go
decoder := json.NewDecoder(strings.NewReader(response))
decoder.DisallowUnknownFields()
decoder.Decode(&wire)
checkRequiredFields()
checkIssues()
checkPassedIssuesInvariant()
```

只在提示词里写“必须返回 JSON”并不等于程序获得了结构化输出。模型仍可能输出代码块、截断文本或错误字段，必须由 Parser 建立可信边界。

### 6.3 Repair 输入

Before：

```text
原 Reviewer 自己发现问题并立即改图，程序没有 issue hand-off。
```

After：

```text
Repair Input = 原始需求 + 分析结果 + 当前 XML + 带来源的问题数组
```

当前候选和本轮问题放在同一个不可歧义的 JSON payload 中，为 Day 4 的状态更新与调用断言提供基础。

## 7. 删除代码说明

本日无实质删除。

`loader_test.go` 中原先重复的真实 YAML 加载代码被提取为 `loadDrawioTable` 测试辅助函数；加载行为、环境变量设置和断言范围没有删除，由两个配置测试共同复用。

原 `agent_reviewer`、`final_result`、`sequential_draw_process` 和 `drawio-xml` 均未删除或替换。

## 8. 一次完整协议运行示例

以下是离线协议级示例，不代表今天已经调用真实模型或跑通自动循环。

成功修复路径：

1. 原始需求是“下单成功后扣减库存”；
2. Analyst 结果是“订单服务调用库存服务”；
3. 当前 XML 结构合法，但没有对应 edge；
4. `reviewer.BuildPrompt` 生成角色指令和 JSON payload；
5. 模型返回 `{"passed":false,... "type":"missing_edge"}`；
6. `reviewer.ParseResponse` 返回 `Passed=false` 和一个 issue，`err=nil`；
7. `repair.FromReviewerIssues` 保留 `source=reviewer` 与 `code=missing_edge`；
8. `repair.BuildPrompt` 把同一份当前 XML 和该问题交给 Repair；
9. Repair 返回增加连线后的完整 XML；
10. `repair.ParseResponse` 检查裸 XML，再调用 Day 2 Validator；
11. 通过后返回完全相同的 XML 字符串。

协议错误路径：

1. Reviewer 返回 Markdown 代码块包裹的 JSON；
2. 严格 Decoder 在第一个字符处失败；
3. 返回 `ProtocolError{Code:"review_protocol_error", Violation:"invalid_json"}`；
4. 该错误不是 `Passed=false`，因此未来控制器不应启动 Repair，也不应自动追加一次模型调用。

坏修复路径：

1. Repair 返回 `<mxfile><diagram/></mxfile>`；
2. 裸 XML 边界通过；
3. Day 2 Validator 发现缺少 `mxGraphModel`；
4. `repair.ParseResponse` 返回包含 `missing_graph_model` 的 `validation.Error`；
5. Day 4 控制器将负责计数并判断是否还有修复预算。

## 9. 测试与证据

改动前的基线命令：

```powershell
go test -count=1 ./...
```

结果：全量通过。

Day 3 局部测试命令：

```powershell
go test -count=1 ./internal/domain/diagram/reviewer ./internal/domain/diagram/repair ./internal/app/config
```

结果：

```text
ok  ai-agent-scaffold/internal/domain/diagram/reviewer
ok  ai-agent-scaffold/internal/domain/diagram/repair
ok  ai-agent-scaffold/internal/app/config
```

测试性质：

- Reviewer/Repair：离线确定性单元测试；
- 配置边界：离线读取真实 YAML；
- 真实模型：本日未调用；
- Draw.io 画布：本日未加载新图；
- HTTP/前端：本日未修改。

最终全量回归结果记录在第 13 节。

## 10. 当前限制与排错

当前限制：

- 新 Reviewer 和 Repair 协议尚未调用 ChatModel；
- 尚无请求级状态、`attempt`、`max_repairs` 或有限循环；
- `review_protocol_error` 尚未映射到 HTTP 业务码，S04/S05 再接；
- 第一版未使用供应商原生 Structured Output，只使用提示约束 + 严格 Parser；
- Reviewer 只能根据 XML/geometry 推断明显布局问题，没有真实渲染视觉；
- `type` 保持可扩展，尚未建立冻结枚举或 severity；
- 仍只支持 Day 2 约定的单页未压缩 Draw.io XML；
- 未做真实模型成功率或成本结论。

排错顺序：

1. Reviewer 被判 `invalid_json`：先保存原始响应用于受控诊断，检查是否带代码块/解释文字，不要在 Parser 中自动提取 JSON；
2. `missing_passed` / `missing_issues`：检查模型是否遵循固定 schema；
3. `inconsistent_result`：检查 `passed` 和问题数组是否相互矛盾；
4. Repair 报 `repair_output_not_bare_xml`：检查前后空白、Markdown 或解释；
5. Repair 报 `missing_graph_model` / `dangling_reference`：进入 Day 2 Validator 的结构问题定位；
6. 配置回归测试失败：确认是否在 S05 之前误改了生产 `sequential_draw_process`。

## 11. 面试表达

30 秒版本：

> 原项目 Reviewer 会在一次模型调用里同时检查和修图，最终只返回 XML，程序无法知道发现了什么问题。我把它拆成只读 Reviewer JSON 协议和独立 Repair XML 协议。Reviewer 用严格 Parser 区分质量不通过与协议错误，Repair 明确接收当前候选和本轮问题，并复用确定性 XML Validator。Day 3 只建立协议和测试，生产入口仍返回 XML，避免半接入。

2 分钟版本：

> 我先核对了真实基线：原 Reviewer 不是普通文本审查，而是检查后直接修图并返回裸 XML；Day 2 已在最终边界增加确定性 Validator。Day 3 解决的是控制权问题。Reviewer 输入同时包含原始需求、分析结果和当前 XML，只返回 passed 与 issues。程序使用 DisallowUnknownFields、指针字段和二次 Decode，拒绝缺字段、错类型、额外内容与 passed/issues 矛盾，并把这些归为 review_protocol_error；合法的 passed=false 只是质量结果。Repair 则接收原始需求、分析、最新 XML 和带 validator/reviewer 来源的问题，返回完整 XML。坏修复仍走 validation.Error，未来有限循环可以消耗预算后决定是否再修。为了控制迁移风险，我增加真实 YAML 回归测试，确认 Day 3 阶段对外仍是旧 Reviewer 返回 XML，等控制器和装配完整后再一次切换。

## 12. 术语、理解题与小练习

术语：

- Structured Output：程序能够按固定字段和类型消费的模型输出；提示词只是约束手段，不等于输出天然可信。
- Schema：字段名称、类型、必填性和结构约定。
- Strict Parsing：拒绝未知字段、尾随内容、缺失字段和错误类型，不做猜测性修复。
- Invariant：所有合法状态都必须满足的关系，本协议是 `passed == (issues 为空)`。
- Quality Rejection：审查成功执行，但候选质量不合格。
- Protocol Error：模型回复无法形成可信审查结果。
- Repair：根据诊断修改当前候选；它不是无条件重复同一次生成。

理解题：

1. 为什么 `passed=false` 不能直接作为 Go error 返回？
2. 为什么 Reviewer 输入既要有原始需求，又要有 Analyst 结果？
3. 为什么 `bool` 和 slice 在 wire 类型中使用指针？
4. 为什么不自动剥掉 Markdown 代码块再解析？
5. `dangling_reference` 和 `missing_edge` 的判断来源有什么不同？
6. 为什么 Repair 的坏 XML 是 `validation.Error`，而 Reviewer 的截断 JSON 是 `review_protocol_error`？

小练习：

在 `TestParseResponseRejectsMalformedOrContradictoryReplies` 中新增一个“issue 含未知字段”的用例，例如：

```json
{
  "passed": false,
  "issues": [
    {
      "type": "missing_edge",
      "description": "缺少边",
      "element_ids": [],
      "severity": "high"
    }
  ]
}
```

先预测它属于哪个 violation，再运行：

```powershell
go test -count=1 ./internal/domain/diagram/reviewer
```

不要把练习完成状态记为“已掌握”，需要实际复述或操作后再更新理解状态。

## 13. 最终验证与下一步

最终需要执行：

```powershell
go test -count=1 ./...
git diff --check
```

实际结果：`go test -count=1 ./...` 所有包通过；`git diff --check` 退出码为 0，没有空白错误。Git 仅提示两个原有 CRLF 工作区文件在索引中使用 LF，这不是 diff 检查失败。

Day 4 / S04 从这里继续：

1. 定义每次请求独立的运行状态；
2. 用模型替身执行 Validator -> Reviewer -> Repair -> Validator；
3. 固定 `attempt=0` 表示初稿、`max_repairs=2` 表示最多调用 Repair 两次；
4. 覆盖首次通过、一次修复、坏修复、预算耗尽、零预算、协议错误、模型错误、取消和状态隔离；
5. 仍不提前改 HTTP Handler，完整真实入口接入留到 S05。
