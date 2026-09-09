# Day 1 基线与案例

这里是最小采集工具与原始证据，不是完整 Eval 评分平台。

## 文件与记录

| 内容 | 位置与含义 |
|---|---|
| 源码快照 | [manifest.json](D:/Projects/GolandProjects/ai-agent-scaffold/evals/baselines/day01-original-20260908/manifest.json)，75 个文件，source 子目录为对应副本 |
| 开发案例 | [dev-v1.json](D:/Projects/GolandProjects/ai-agent-scaffold/evals/cases/dev-v1.json)，10 条，不是冻结验收集 |
| 连通性探测 | [probe.json](D:/Projects/GolandProjects/ai-agent-scaffold/evals/runs/day01-probe-20260908/probe.json)，gpt-5.5 不受支持；两个 MCP TCP 端口未连通 |
| 网关公布目录 | [models.json](D:/Projects/GolandProjects/ai-agent-scaffold/evals/runs/day01-model-catalog-20260908/models.json)，未逐个验证模型可用性 |
| 首条绘图尝试 | [dev-01-login.json](D:/Projects/GolandProjects/ai-agent-scaffold/evals/runs/day01-original-gpt55-20260908/dev-01-login.json)，Analyst 发生 EOF，没有图纸 |
| 候选模型探测 | day01-probe-deepseek-v4-pro-20260908 与 day01-probe-glm-5.3-flash-20260908，模型名被接受，但上游均返回 429 限流 |

快照在新增采集命令之前建立，不含新命令本身。恢复时把 source 复制到独立目录并按 manifest 校验哈希，提供本地环境配置；不要覆盖当前工作目录。两份含字面凭据的 YAML 副本脱敏，原文件未改。.env、IDE、依赖缓存和评测结果未进入快照。

## 运行方法

在 D:\Projects\GolandProjects\ai-agent-scaffold 执行，读取现有 .env，系统环境变量优先；不打印 API Key。

    # 不联网、不写文件
    go run ./cmd/baseline -check

    # 短模型请求 + MCP TCP 检查（不是 MCP 握手）
    go run ./cmd/baseline -probe -timeout 30s -out evals/runs/my-probe-01

    # 只读获取网关公布模型
    go run ./cmd/baseline -models -timeout 30s -out evals/runs/my-model-list-01

    # 当前配置模型，单条案例
    go run ./cmd/baseline -case dev-01-login -out evals/runs/my-original-01

    # 明确模型选择后，将 MODEL_ID 替换为实际名称；仅覆盖本次采集，不改 YAML
    go run ./cmd/baseline -model MODEL_ID -case all -out evals/runs/my-original-all-01

输出目录必须是新目录，拒绝覆盖。默认单条；-case all 才顺序跑全部 10 条。首次错误时停止并保留已写记录，其余为未执行，不可计作完成。

## profile 与统计边界

original-no-tools 保留原指令和 Agent/Runner，只在采集器内存配置中清空工具列表；10 条案例均不需要检索。这不代表已跑通含 MCP 的完整原环境。

后续对照必须使用相同模型、工具配置、案例、超时和评分规则。通过 -model 换模型后，只能在新模型条件下建立对照，不能把模型差异归因于 Repair。metadata 记录实际模型；后续新运行同时记录原模型与覆盖值。

- metadata 保存配置、案例、运行时、模型客户端、依赖的哈希及模型/工具/超时信息。
- model_calls 保存当前模型端口暴露的阶段输入输出，不是完整 HTTP 原始响应。
- status=returned 仅表示调用返回，不代表质量合格；quality_evaluated=false 表示未评分。
- *-output.txt 是未经过 Validator 的原样文本。
- usage=null 代表供应商 usage 未被当前端口保留，不是 0 Token。
- 耗时为 CLI 内 Runner/模型耗时，不包含网页、浏览器或端到端产品延迟。

采集沿用原 Runner 的 Background Context，默认每次模型请求超时 5 分钟；未实现新工作流的整体取消，不能据此认定 Day 6 完成。

## 当前结论

用户确认项目在其正常启动环境中可通过中转站运行。Codex 当前采集环境已保存不同请求的失败证据：gpt-5.5 短探测为 model_not_found，首条绘图为 EOF，两个候选模型当前均为上游 429 限流。这些结果只描述本次自动采集时段，不能据此判断项目整体不能运行。暂无自动采集的成功图纸和通过率；限流恢复后继续，模拟结果不算 Before 成绩。
