# S02：Draw.io XML Validator

> 冲刺日：Day 2  
> 实施日期：2026-09-08  
> 代码状态：已完成  
> 验证状态：离线测试通过  
> 理解状态：待复述

## 1. 今天解决的具体问题

YAML 已经明确要求 `agent_reviewer` 检查并修正上一步图纸，最终只输出一份裸 Draw.io XML；正常情况下，它输出的内容就是 XML。这里增加 Validator 不是因为 Reviewer 被设计成输出普通文本，而是因为这条输出约定此前只写在提示词中。

从 Go 运行时看，模型响应统一保存在 `ChatReply.Content string` 中，顺序工作流直接把最后一个 Agent 的这个字符串作为最终结果返回。原后端没有再用 XML Parser 验证该字符串是否满足提示词约定；前端的 `isDrawIoXmlContent` 也只是检查 `<mxfile`、`<mxCell` 等字符串特征。因此更准确地说：**原流程在程序层信任 Reviewer 会遵守“输出合法 XML”的提示词契约；如果某一次模型响应没有完全遵守，原流程缺少确定性拦截。** 未闭合标签、代码块、重复 ID 和悬空引用是 Validator 要防守的可能失败类型，并不是在 Day 1 已经证实你的 Reviewer 经常这样输出。

Day 2 在 Reviewer 最终输出与 Runner 成功返回之间增加确定性 Guardrail：

```text
Analyst -> Drawer -> Reviewer -> Draw.io Validator
                                     | passed=true  -> 返回 XML
                                     | passed=false -> 返回 0004 + issues
```

Validator 是普通 Go 代码，不调用模型。同一份 XML 会得到同一顺序的检查结果。

## 2. 为什么 Reviewer 之后还要有 Validator

Reviewer 擅长理解语义，例如“支付成功后是否缺少库存扣减”，并且 YAML 已要求它最终返回 XML。不过这个要求属于自然语言提示词契约；模型通常会遵守，但程序不能把提示词本身当作已经执行过的 XML 解析和引用校验。

Validator 擅长判断可以写成明确规则的问题，例如标签是否闭合、ID 是否重复、`source` 是否真的存在。它不能判断业务需求是否满足。

两者的职责可以这样区分：

| 问题 | Validator | Reviewer |
|---|---:|---:|
| XML 标签没有闭合 | 能可靠判断 | 可能判断，但输出仍可能出错 |
| `source="service-a"`，却没有该 ID | 能可靠判断 | 可能漏掉 |
| 用户要求支付服务，但图中没画 | 无法只靠 XML 规则判断 | 应负责判断 |
| 节点是否美观、关系是否符合业务 | 第一版不判断 | 应负责判断 |

因此“XML 合法”只是交付图纸的必要条件，不等于“图纸正确”。Day 3 会把 Reviewer 改成结构化语义审查，Day 4 再让 Repair 消费两类问题。

## 3. 当前接受的 Draw.io 格式

第一版只接受当前提示词要求的单页未压缩结构：

```xml
<mxfile>
  <diagram>
    <mxGraphModel>
      <root>
        <mxCell id="0" />
        <mxCell id="node-a" vertex="1" parent="0">
          <mxGeometry x="20" y="20" width="120" height="60" />
        </mxCell>
      </root>
    </mxGraphModel>
  </diagram>
</mxfile>
```

当前明确不支持：

- 多个 `<diagram>` 的多页图纸；
- `<diagram>` 中保存压缩文本的格式；
- `<object>`、`<UserObject>` 等包裹 `mxCell` 的格式。

这些格式不会被静默跳过，而会返回 `unsupported_*` 问题。这样可以区分“图纸真的坏了”和“Validator 第一版还没有覆盖这种格式”。

## 4. 规则与结构化结果

主要规则如下：

1. 输出必须是完整的一份 XML，根元素必须是 `mxfile`。
2. 必须存在一条 `mxfile -> diagram -> mxGraphModel -> root` 路径。
3. 每个直接位于 `root` 下的 `mxCell` 必须有非空且不重复的 `id`。
4. `parent`、`source`、`target` 存在时，引用的 ID 必须存在。
5. `edge="1"` 必须同时具有 `source` 与 `target`。
6. `vertex="1"` 必须有且只有一个直接子元素 `mxGeometry`；`width`、`height` 必须是正的有限数字；可选的 `x`、`y` 若存在则必须是有限数字。

失败时领域层返回的结构类似：

```json
{
  "validator": "drawio-xml",
  "result": {
    "passed": false,
    "issues": [
      {
        "code": "dangling_reference",
        "message": "source references unknown mxCell id \"service-a\"",
        "path": "/mxfile/diagram[1]/mxGraphModel/root/mxCell[id=edge-1]",
        "elementId": "edge-1",
        "attribute": "source"
      }
    ]
  }
}
```

同步 HTTP 接口会返回业务错误码 `0004`，并把上述对象放在 `data` 中。失败 XML 不会出现在成功响应的 `content` 中。

## 5. 关键代码数据流

```text
agent-draw-io.yaml
  runner.output-validator: drawio-xml
        |
RunnerConfig.OutputValidator
        |
RunnerNode.NewRunner(..., outputValidatorName)
        |
adk.Factory.resolveValidator
        |
Runner.Run / Runner.Stream
        |
Runner.validateOutput
        |
drawio.Validator.Validate
        | passed                 | rejected
返回原 XML                 validation.Error
                                  |
                         HTTP code=0004 + data
```

关键文件：

- `internal/domain/diagram/drawio/validator.go`：解析 XML、检查结构、ID、引用和 geometry。
- `internal/domain/validation/validation.go`：通用的 `Validator`、`Result`、`Issue` 和可跨层传递的错误。
- `internal/infrastructure/adk/adapter.go`：注册校验器，并在最终输出返回前执行。
- `internal/domain/agent/model/config.go`：增加 `runner.output-validator` 配置字段。
- `internal/trigger/http/agent_handler.go`：把校验失败映射成 `0004` 和结构化数据。
- `configs/agent/agent-draw-io.yaml`：只给绘图应用启用 `drawio-xml`；普通 Agent 不受影响。

## 6. 改动总表与逐代码块讲解

建议按以下顺序打开代码对照阅读：

1. [通用校验协议](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/domain/validation/validation.go:6)：先理解 Issue、Result、接口和领域错误。
2. [Draw.io 校验入口](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/domain/diagram/drawio/validator.go:30)：再沿 `Validate -> parseDocument -> validateMXFile -> validateCells` 阅读。
3. [Runner 配置字段](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/domain/agent/model/config.go:97)和 [Runner 组装传参](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/domain/agent/service/armory/runner_node.go:42)：观察 YAML 如何进入运行时。
4. [Factory 注册与构造](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/infrastructure/adk/adapter.go:61)、[同步返回边界](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/infrastructure/adk/adapter.go:477)、[流式返回边界](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/infrastructure/adk/adapter.go:508)：理解 Guardrail 放置位置。
5. [HTTP 错误映射](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/trigger/http/agent_handler.go:156)与 [0004 错误码](/D:/Projects/GolandProjects/ai-agent-scaffold/pkg/types/codes.go:12)：理解结构化问题如何到达客户端。
6. [绘图 YAML](/D:/Projects/GolandProjects/ai-agent-scaffold/configs/agent/agent-draw-io.yaml:106)和 [Before 基线隔离](/D:/Projects/GolandProjects/ai-agent-scaffold/cmd/baseline/main.go:224)：理解配置生效范围与评测口径。
7. [Validator 测试](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/domain/diagram/drawio/validator_test.go:14)、[Runner 测试](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/infrastructure/adk/adapter_test.go:284)、[配置测试](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/app/config/loader_test.go:9)和 [HTTP 测试](/D:/Projects/GolandProjects/ai-agent-scaffold/internal/trigger/http/agent_handler_test.go:15)：把规则与证据对应起来。

### 6.1 改动总表

| 文件 | 类型 | 改动前 | 改动后 | 为什么要改 |
|---|---|---|---|---|
| `internal/domain/validation/validation.go` | 新增 | 没有统一的输出校验协议 | 定义 `Issue`、`Result`、`Validator`、`Error` | 让 Draw.io 校验结果能被 Runner、HTTP 和未来 Repair 共同使用 |
| `internal/domain/diagram/drawio/validator.go` | 新增 | 后端不解析最终 XML | 用确定性代码检查语法、结构、ID、引用和 geometry | 提示词与 LLM Reviewer 无法提供确定性保证 |
| `internal/domain/agent/model/config.go` | 修改 | Runner 只能配置 Agent 与 Plugin | 增加可选 `OutputValidator` | 让不同应用按需启用 Guardrail，普通 Agent 保持原行为 |
| `internal/domain/agent/ports/ports.go` | 修改 | `NewRunner` 不知道输出校验器 | 构造参数增加校验器名称 | 把配置从领域组装层传到基础设施实现 |
| `internal/domain/agent/service/armory/runner_node.go` | 修改 | 创建 Runner 时只传插件 | 同时传入 `runnerConfig.OutputValidator` | 打通 YAML 到 Runner 的配置链路 |
| `internal/infrastructure/adk/adapter.go` | 修改 | Runner 直接返回 Agent 输出 | Factory 注册校验器，Runner 返回前验证完整输出 | 把最终交付边界变成统一质量门禁 |
| `configs/agent/agent-draw-io.yaml` | 修改 | 绘图结果不启用代码校验 | 配置 `output-validator: drawio-xml` | 只对 Draw.io 工作流启用本规则 |
| `pkg/types/codes.go` | 修改 | 校验失败只能落入未知错误 `0001` | 增加 `0004` | 让前端和日志能区分输出质量失败与运行异常 |
| `internal/api/response/response.go` | 修改 | 失败响应只能带 `code/info` | 增加 `FailureWithData` | 返回 Repair 和前端都能解析的 issues |
| `internal/trigger/http/agent_handler.go` | 修改 | 非 `AppError` 都映射成未知错误 | 优先识别 `validation.Error` | 防止结构化诊断在 HTTP 边界丢失 |
| `cmd/baseline/main.go` | 修改 | 自动继承当前 YAML 的所有 Runner 行为 | Before 采集时显式清空 `OutputValidator` | 保持 Day 1 改造前基线口径不受 Day 2 影响 |
| 四组测试文件 | 新增/修改 | 没有这些规则的回归证据 | 覆盖纯校验、Runner、配置和 HTTP | 证明错误确实被最终返回边界拦住 |

### 6.2 通用校验协议：`validation.go`

#### `Issue`

`Issue` 表示一个可以定位的确定性问题。`Code` 给程序分支使用，例如 `duplicate_id`；`Message` 给人阅读；`Path` 指向 XML 位置；`ElementID` 和 `Attribute` 让未来 Repair 知道要处理哪个元素的哪个属性。这里没有直接使用字符串数组，因为只有文字描述很难稳定统计，也不方便 Repair 精确消费。

#### `Result`

`Result` 把总判断 `Passed` 和详细 `Issues` 放在一起。调用方不需要通过“错误字符串里是否包含某个词”判断类型。成功要求 `Passed=true` 且问题数组为空；失败保留全部已发现的问题，便于一次修复多个确定性错误。

#### `Validator` 接口

接口只有 `Name()` 与 `Validate(output)`。`Name` 对应 YAML 配置名，`Validate` 只接收完整最终字符串，因此实现不依赖 HTTP、具体 Agent 或模型供应商。以后如果增加 Mermaid 或 JSON Validator，可以注册新实现，不需要把 Draw.io 判断写进通用 Runner。

#### `validation.Error`

`Error` 同时保存 Validator 名称和完整 `Result`，并实现 Go 的 `error` 接口。这样下层可以按普通错误向上传播，HTTP 层又能用 `errors.As` 取回结构化字段。`Error()` 只生成简短摘要用于日志，真正给程序消费的数据仍在 `Result`，避免解析错误文本。

### 6.3 Draw.io 校验器：`validator.go`

#### `NewValidator`、`Name` 与 `Validate`

`NewValidator` 创建无状态实例，所以同一个实例可以安全地被多个 Runner 调用。`Name` 固定返回 `drawio-xml`，必须和 YAML 完全一致。

`Validate` 的执行顺序是：

```text
parseDocument
  -> 语法失败：立即返回 invalid_xml 等解析问题
  -> 语法成功：validateMXFile
       -> issues 为空：Passed=true
       -> issues 非空：Passed=false
```

解析和领域检查分开，是因为“标签没闭合”和“标签闭合但没有 diagram”属于不同失败原因，后续 Repair 策略也可能不同。

#### `xmlNode`

`xmlNode` 是校验器内部的轻量 XML 树，只保存名称、属性、子节点和直接文本。这里没有直接定义一套固定的 `mxfile` Go struct，因为固定反序列化可能静默忽略未知节点；本项目要求对象包装等未支持格式必须明确失败，所以需要保留实际出现的元素再判断。

#### `parseDocument`

该函数先拒绝纯空白输出，然后用 `encoding/xml.Decoder` 逐个读取 token。`stack` 保存当前从根到正在读取元素的路径：

- 遇到 `StartElement`：创建节点；栈为空时它是文档根，否则挂到当前父节点；已有根时又遇到顶层元素，就返回 `multiple_documents`。
- 遇到 `EndElement`：弹出当前节点，回到父级。
- 遇到 `CharData`：在根外只允许空白，防止模型在 XML 前后加解释文字；在元素内保存文本，用于识别压缩 diagram。
- Decoder 返回语法错误：转换成 `invalid_xml`，例如未闭合标签或未转义 `&`。

它只负责回答“是不是一份完整 XML”并建立树，不在这里混入 Draw.io ID 规则，使每层职责清楚。

#### `validateMXFile`

这个函数从外到内检查第一版结构：根必须是 `mxfile`；必须且只能有一个 `diagram`；`diagram` 必须且只能有一个 `mxGraphModel`；模型必须且只能有一个 `root`。

当 `diagram` 没有 `mxGraphModel` 却有非空文本时，返回 `unsupported_compressed_diagram`，而不是笼统地说缺结构。多个 `diagram` 返回 `unsupported_multi_page`。这些错误说明输入可能是 Draw.io 支持的另一种格式，只是当前项目契约没有覆盖。

#### `cellRecord` 与 `validateCells`

`cellRecord` 把节点指针、清理后的 ID 和定位路径保存在一起，避免后续每条规则重复拼路径。

`validateCells` 第一遍完成三件事：

1. 检查 `root` 的直接子元素是不是 `mxCell`；对象包装会得到 `unsupported_cell_wrapper`。
2. 建立按文档顺序排列的 `cells`，保证输出问题顺序稳定。
3. 建立 `ids` 查询表，同时检查 `missing_id` 和 `duplicate_id`。

第二遍遍历 `cells`：先调用 `validateCellShape`，再检查 `parent/source/target`。引用值存在但不在完整 `ids` 表中时，生成 `dangling_reference`。map 只用于“是否存在”查询，没有遍历 map 生成 issues，因此相同输入不会因为 Go map 顺序随机而改变结果顺序。

#### `validateCellShape`

该函数先读取 `vertex="1"` 和 `edge="1"`：

- 两者同时为 1 时返回 `conflicting_cell_kind`，因为一个 cell 不能同时表示节点和边。
- Edge 必须同时有 `source` 与 `target`；缺失时产生 `missing_edge_endpoint`。端点值是否存在由外层第二遍统一检查。
- 非 Vertex 不继续要求节点 geometry，避免把根 cell、分组 cell 或边的可选结构误判成节点错误。
- Vertex 必须有且只有一个直接 `mxGeometry`；宽高必须是正有限数；可选的 x/y 出现时必须是有限数。

这里没有检查颜色、样式、重叠和连线交叉，因为这些不是当前纯结构规则能可靠证明的内容。

#### 辅助函数

`directChildren` 只找直接子节点，防止嵌套在错误位置的元素被当作正确结构；`attr` 统一读取属性；`finiteNumber` 同时排除解析失败、`NaN` 和无穷值；`oneIssue` 统一构造单一早退问题；`failed` 保证解析失败也返回同一种 `Result`。

### 6.4 配置如何进入 Runner

#### `RunnerConfig.OutputValidator`

该字段映射 YAML 的 `output-validator`。它是可选字段：空值表示保持原 Runner 行为，有值表示选择一个已注册的 Guardrail。设计成配置而不是硬编码 Agent 名称，可以避免通用框架层判断“如果 agent ID 是 300000 就校验 XML”。

#### `RunnerFactory.NewRunner` 与 `RunnerNode.Apply`

接口改动前：

```go
NewRunner(ctx, appName, agent, pluginNames)
```

改动后：

```go
NewRunner(ctx, appName, agent, pluginNames, outputValidatorName)
```

`RunnerNode.Apply` 从 `table.Module.Runner` 读取该字段并继续传递。这里修改接口是为了让依赖方向仍然从领域端口指向实现；Runner 不需要自己读取 YAML 文件，也不会和配置文件路径耦合。

### 6.5 Factory 与 Runner 的最终输出门禁

#### `Factory.validators`、`NewFactory` 与 `resolveValidator`

Factory 新增“名称到 Validator 实例”的注册表。`NewFactory` 注册 `drawio-xml`；`resolveValidator` 对空字符串返回 nil，对未知名称立即报错。立即失败比收到用户请求后才发现拼错配置更容易定位，也避免服务带着无效配置启动。

#### `Runner.validator`

Runner 保存已经解析好的接口实例。请求执行时无需重复查名称或创建对象，且 Runner 仍然只依赖通用 `validation.Validator`，不知道 Draw.io 内部规则。

#### `Runner.Run`

改动前的核心行为：

```text
执行 Agent -> output 为空则返回空数组 -> 否则直接成功返回 output
```

改动后的行为：

```text
执行 Agent -> validateOutput
             | 失败：返回 validation.Error，不返回 outputs
             | 通过：维持原空值/单结果返回方式
```

Validator 放在 Agent 完整执行之后，因此当前 Reviewer 的输出正好成为候选图纸；放在 HTTP 层会让其他入口绕过规则，放在 Reviewer 内部则会把确定性基础设施与模型角色混在一起。

#### `Runner.Stream`

XML 必须看到完整文档才能判断标签是否闭合，所以启用 Validator 时，Stream 分支先用同步 `impl.run` 得到完整结果，校验通过后才向 `outputs` Channel 发送一次。失败只进入错误 Channel，任何 XML 片段都不会提前泄漏为成功结果。

当前顺序工作流原本也是完成后输出一个最终字符串，所以这不会丢失已有的阶段事件；项目此时本来就没有阶段事件。Day 7—10 会新增独立的工作流事件流，而不是把未经校验的 XML token 当成进度。

#### `validateOutput`

空 Validator 直接返回 nil，保证普通 Agent 兼容。启用时调用 `Validate`；`Passed=true` 继续，失败则包装成 `validation.Error`。这里集中创建领域错误，确保同步和流式入口使用完全相同的判断。

### 6.6 HTTP 错误如何保留 issues

`CodeOutputValidationFailed="0004"` 给这类失败独立分类。`FailureWithData` 是对原 `Failure` 的补充：普通错误仍可只返回 code/info，Validator 失败可以额外携带结构化对象，没有删除或强迫所有旧调用方修改。

`writeError` 先用 `errors.As` 判断 `validation.Error`，再判断原有 `AppError`，最后才落入未知错误。如果仍沿用原顺序并让校验错误进入兜底，它会变成 `0001` 且只剩错误文字，未来 Repair 和前端无法可靠取得 issue code、元素 ID 与属性。

### 6.7 YAML 与 Before 基线隔离

绘图 YAML 增加：

```yaml
runner:
  agent-name: sequential_draw_process
  output-validator: drawio-xml
```

只有 `agent-draw-io.yaml` 配置它，`only-one-agent.yaml` 为空，因此普通聊天 Agent 的文本不会被误当成 XML 校验。

Day 1 基线采集器读取同一份当前 YAML。若不额外处理，今天新增的 Validator 会拒绝历史测试中故意保留的原始 Reviewer 输出，Before 就不再代表改造前行为。因此 `assembleOriginal` 在内存副本中执行：

```go
table.Module.Runner.OutputValidator = ""
```

它不修改生产 YAML，只隔离基线采集口径。这项兼容处理是在第一次全量回归发现基线测试失败后补上的，说明横切式 Guardrail 需要检查评测工具是否也复用了生产组装链路。

### 6.8 测试代码块分别证明什么

- `validator_test.go` 的合法样例证明支持的最小 Draw.io 结构能通过；表格测试逐个固定错误码；前向引用测试证明两阶段算法；稳定顺序测试防止未来误用 map 遍历。
- `adapter_test.go` 分别证明同步合法结果通过、同步坏结果没有 outputs、流式坏结果没有任何输出、未知 Validator 在组装时失败。
- `loader_test.go` 读取真实 `agent-draw-io.yaml`，证明字段拼写和层级能映射到 `RunnerConfig`。
- `agent_handler_test.go` 经过真实 JSON 编码，证明 `0004`、Validator 名称和 issues 没在 HTTP 边界丢失。
- `cmd/baseline/main_test.go` 保留故意不合法的旧输出，证明 Before 工具仍逐字记录原 Reviewer 结果。

### 6.9 删除代码说明

本日没有删除任何原有功能或公共响应方法。修改以新增字段、扩展构造参数和在返回路径插入校验为主。原 `Failure` 被保留，新增 `FailureWithData`；原无 Validator 的 Runner 行为也通过空配置继续保留。被替换的只是相关函数内部的旧直返步骤，新的对应步骤是“先 `validateOutput`，通过后再按原方式返回”。

## 7. 两阶段引用检查

XML 中的连线可能写在它引用的节点之前：

```xml
<mxCell id="edge-1" edge="1" source="node-a" target="node-b" />
<mxCell id="node-a" ... />
<mxCell id="node-b" ... />
```

如果边解析边检查，看到 `edge-1` 时还没有看到两个节点，会误报悬空引用。实现采用两阶段算法：

1. 第一遍按文档顺序收集所有 `mxCell` 和 ID，同时检查空 ID、重复 ID和不支持的包装格式。
2. 第二遍检查节点类型、geometry 与所有引用。

问题数组始终按遍历顺序追加，不遍历 map 生成问题，因此结果顺序稳定。map 只用来做 ID 是否存在的查询。

## 8. 为什么使用 XML Parser

字符串包含或正则只能回答“看起来有没有 `<mxfile>`”，无法可靠处理标签嵌套、转义字符、属性顺序和未闭合标签。`encoding/xml.Decoder` 会按 XML 语法读取 token，例如 `value="开始 &amp; 准备"` 可以合法解析，而未转义的 `&` 会被拒绝。

解析成功仍不等于 Draw.io 结构可用，所以解析之后还要执行领域规则。

## 9. 一次完整运行示例

假设用户请求“画一个登录流程图”，当前 Analyst、Drawer、Reviewer 仍按原顺序运行。Reviewer 最后产生一份包含 `node-input`、`node-check` 和 `edge-1` 的 XML。

成功路径按真实代码顺序执行：

1. `/chat` 的 `chatMessage` 把请求交给 `chat.Service.HandleMessage`。
2. Service 找到绘图应用的 Runner，调用 `Runner.Run`。
3. 顺序工作流依次执行三个 Agent，最后把 Reviewer 字符串作为 `output` 返回。
4. `Runner.Run` 调用 `validateOutput`，后者调用已注册的 `drawio.Validator.Validate`。
5. `parseDocument` 建树成功；`validateMXFile` 找到单页标准结构；`validateCells` 第一遍收集 ID，第二遍确认 `edge-1` 的 source/target 都存在；节点 geometry 也有效。
6. `Result` 为 `passed=true, issues=[]`，Runner 按原接口返回同一份 XML，HTTP 包装成成功响应，前端才取得图纸。

失败路径只改一个属性：`edge-1` 写成 `target="node-missing"`。前五步相同，但 `validateCells` 第二遍在完整 ID 表中找不到该值，生成：

```json
{
  "code": "dangling_reference",
  "elementId": "edge-1",
  "attribute": "target"
}
```

`validateOutput` 将失败结果包装为 `validation.Error`。`Runner.Run` 返回 `outputs=nil` 和该错误，所以失败 XML 不会进入成功数据。`writeError` 再把它变为业务码 `0004` 和结构化 `data`。Day 2 到这里停止；Day 4 的 Repair Loop 会取得这些 issues、修改候选 XML，然后重新从 Validator 开始检查。

## 10. 测试与真实验证边界

已运行：

```powershell
go test ./internal/domain/diagram/drawio ./internal/infrastructure/adk ./internal/app/config ./internal/trigger/http
```

覆盖行为：

- 合法单页 XML 通过；
- 空输出、未闭合 XML、多个 XML 根被拦截；
- 多页、压缩内容和对象包装被明确标为不支持；
- 空 ID、重复 ID、悬空引用、缺少连线端点被拦截；
- 连线先于节点出现仍可通过；
- 缺失或非法 geometry 被拦截；
- 相同输入的问题顺序稳定；
- Runner 同步与流式入口都不会发出坏 XML；
- Draw.io YAML 能正确加载 `drawio-xml`；
- HTTP 响应保留结构化 issues；
- 未注册的 Validator 名称会在创建 Runner 时失败。

这些是本地确定性测试。本日没有依赖中转站做真实模型调用，也没有宣称真实图纸质量已经提升。最终又运行了 `go test ./...`，全项目通过。Day 1 的 Before 基线工具会显式关闭后来新增的 Validator，避免历史基线被 Day 2 行为污染。你的模型和中转站配置没有被改动。

## 11. 当前限制与排错

- Day 2 只负责“拦住”，还不会自动修复；Repair Loop 在 Day 4 完成。
- Reviewer 目前仍同时检查并修改 XML；Day 3 会拆分审查协议与 Repair 模板。
- 当前 Validator 不验证需求是否完整、布局是否重叠、连线是否美观。
- 当前流式绘图结果必须先聚合成完整 XML 再校验，因此只会在通过后发最终图纸；真正的阶段事件流在 Day 7—10 实现。
- 单页、未压缩和直接 `mxCell` 是项目第一版的输入契约，不代表 Draw.io 只支持这些格式。

遇到 `0004` 时可按 issue code 排查：`invalid_xml` 先查看模型是否输出代码块、解释文字、未闭合标签或未转义字符；`missing_*` 查看结构或必需属性；`duplicate_id` 和 `dangling_reference` 使用 `elementId/attribute/path` 定位。若服务启动时提示 validator 未注册，先核对 YAML 名称和 Factory 注册名是否一致。若普通文本 Agent 意外被 XML 校验，检查它的 runner 是否误配了 `output-validator`。

## 12. 面试表述

30 秒版本：

> 我在 LLM Reviewer 的最终输出后增加了确定性 Draw.io Guardrail。它用 XML Parser 检查结构、ID、引用和节点 geometry，失败时返回可定位的 issues，不让坏 XML进入成功响应。引用检查分两遍，所以支持前向引用；整个过程不增加模型调用。

2 分钟版本：

> 原流程让 LLM Reviewer 直接输出最终 Draw.io XML，但提示词不能保证格式和引用一定合法。我先定义了通用 Validator 接口、结构化 Result 和可跨层传播的 validation.Error，再实现单页未压缩 Draw.io 校验器。解析层用 encoding/xml 判断文档是否合法，领域层检查 mxfile 到 root 的结构、mxCell ID、parent/source/target 和 vertex geometry。引用检查先收集完整 ID，再做第二遍查询，既支持前向引用，也让问题顺序保持稳定。Runner 通过 YAML 按应用选择 Validator，在同步和流式最终返回边界统一拦截；HTTP 使用独立的 0004 业务码保留结构化 issues。普通 Agent 的配置为空，所以维持旧行为；Day 1 Before 采集器也显式关闭 Guardrail，防止改造后逻辑污染基线。这一步只证明格式和结构满足当前契约，业务完整性仍交给后续结构化 Reviewer。

## 13. 术语、理解题与小练习

- **Guardrail：** 在不可信或概率性输出进入下一层前执行的约束与检查。
- **确定性检查：** 相同输入在相同规则下得到相同结果，不依赖模型采样。
- **前向引用：** 当前元素引用文档中稍后才出现的 ID。
- **悬空引用：** `parent/source/target` 指向不存在的 ID。
- **Fail fast：** 配置错误尽早在组装阶段失败，避免拖到真实请求期间。

你需要能回答：

1. 已经有 Reviewer，为什么还要写 Validator？
2. 为什么引用检查要分两遍？如果只检查已经遍历到的 ID，会发生什么？
3. `passed=true` 能证明什么，不能证明什么？

可选小练习：把合法样例中 `target="b"` 改成 `target="missing"`，先预测返回的问题码、`elementId` 和 `attribute`，再运行对应包测试验证。





