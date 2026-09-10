package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"ai-agent-scaffold/internal/domain/agent/model"
	diagramworkflow "ai-agent-scaffold/internal/domain/diagram/workflow"

	"gopkg.in/yaml.v3"
)

type agentRoot struct {
	AI struct {
		Agent struct {
			Config struct {
				Tables map[string]model.AiAgentConfigTable `yaml:"tables"`
			} `yaml:"config"`
		} `yaml:"agent"`
	} `yaml:"ai"`
}

func LoadAgentTables(data []byte) (map[string]model.AiAgentConfigTable, error) {
	//将配置文件中的环境变量占位符替换为实际的值
	expanded := expandEnvPlaceholders(string(data))

	var root agentRoot
	if err := yaml.Unmarshal([]byte(expanded), &root); err != nil {
		return nil, fmt.Errorf("parse agent config: %w", err)
	}
	//获取实际需要的 AiAgentConfigTable 结构体
	tables := root.AI.Agent.Config.Tables
	if len(tables) == 0 {
		return nil, fmt.Errorf("agent config tables are required")
	}
	//对每个完整的agent应用配置进行默认值配置、业务层面的validate，再回写回去
	for name, table := range tables {
		normalizeDefaults(&table)

		if err := validateTable(name, table); err != nil {
			return nil, err
		}
		//上面 修改的是当前副本，不是 map 中原来的值，所以需要再添加默认之后重新写入
		tables[name] = table
	}
	return tables, nil
}

func LoadAgentTablesFile(path string) (map[string]model.AiAgentConfigTable, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf(
			"read agent config %s: %w",
			path,
			err,
		)
	}

	return LoadAgentTables(data)
}

// 匹配两种环境变量占位符：
//
//	${VAR}
//	${VAR:-default}
//
// groups[1] 是环境变量名，groups[2] 是默认值。
var envPlaceholderRE = regexp.MustCompile(
	`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`,
)

// expandEnvPlaceholders 将配置文本中的环境变量占位符替换成实际值。
//
// 替换规则：
//  1. 环境变量存在且不为空：使用环境变量值。
//  2. 环境变量不存在或为空：使用占位符中的默认值。
//  3. 没有默认值：替换为空字符串。
func expandEnvPlaceholders(input string) string {
	// 查找 input 中所有符合正则的占位符，
	// 每匹配到一个，就调用一次匿名函数生成替换结果。
	return envPlaceholderRE.ReplaceAllStringFunc(
		input,
		func(match string) string {
			// 例如 match 为 ${DB_PORT:-3306}：
			// groups[1] = DB_PORT
			// groups[2] = 3306
			groups := envPlaceholderRE.FindStringSubmatch(match)
			name := groups[1]

			// 优先读取系统环境变量。
			if value, ok := os.LookupEnv(name); ok && value != "" {
				return value
			}

			// 环境变量不存在或为空时，返回配置中的默认值。
			// 未配置默认值时，groups[2] 为空字符串。
			if len(groups) > 2 {
				return groups[2]
			}

			return ""
		},
	)
}

// 补充默认值
func normalizeDefaults(table *model.AiAgentConfigTable) {
	if table.Module.AiAPI.CompletionsPath == "" {
		table.Module.AiAPI.CompletionsPath = "v1/chat/completions"
	}

	if table.Module.AiAPI.EmbeddingsPath == "" {
		table.Module.AiAPI.EmbeddingsPath = "v1/embeddings"
	}

	for i := range table.Module.ChatModel.ToolSkillsList {
		if table.Module.ChatModel.ToolSkillsList[i].Type == "" {
			table.Module.ChatModel.ToolSkillsList[i].Type = "directory"
		}
	}

	for i := range table.Module.AgentWorkflows {
		workflow := &table.Module.AgentWorkflows[i]
		// max-iterations 只属于旧 loop 工作流；不能把它静默当成质量修复预算。
		if workflow.Type == model.WorkflowTypeLoop && workflow.MaxIterations == 0 {
			workflow.MaxIterations = 3
		}
		if workflow.Type == model.WorkflowTypeDrawIORepair && workflow.MaxRepairs == nil {
			defaultMaxRepairs := diagramworkflow.DefaultMaxRepairs
			workflow.MaxRepairs = &defaultMaxRepairs
		}
	}
}

// 业务规则上面的table表的校验
func validateTable(
	name string,
	table model.AiAgentConfigTable,
) error {
	prefix := "agent table " + name

	//必填项
	required := map[string]string{
		"app-name":                 table.AppName,
		"agent.agent-id":           table.Agent.AgentID,
		"module.ai-api.base-url":   table.Module.AiAPI.BaseURL,
		"module.ai-api.api-key":    table.Module.AiAPI.APIKey,
		"module.chat-model.model":  table.Module.ChatModel.Model,
		"module.runner.agent-name": table.Module.Runner.AgentName,
	}

	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf(
				"%s: %s is required",
				prefix,
				field,
			)
		}
	}
	//至少配置了一个agent
	if len(table.Module.Agents) == 0 {
		return fmt.Errorf(
			"%s: module.agents is required",
			prefix,
		)
	}
	agentNames := make(map[string]struct{}, len(table.Module.Agents))
	for i, agent := range table.Module.Agents {
		//检查 Agent 名称
		agentName := strings.TrimSpace(agent.Name)
		if agentName == "" {
			return fmt.Errorf(
				"%s: module.agents[%d].name is required",
				prefix,
				i,
			)
		}
		if _, exists := agentNames[agentName]; exists {
			return fmt.Errorf(
				"%s: module.agents[%d].name is duplicated: %s",
				prefix,
				i,
				agentName,
			)
		}
		agentNames[agentName] = struct{}{}
		//检查agent简要说明，Agent 的 instruction 一般就是系统提示词
		if strings.TrimSpace(agent.Instruction) == "" {
			return fmt.Errorf(
				"%s: module.agents[%d].instruction is required",
				prefix,
				i,
			)
		}
	}

	availableNames := make(map[string]struct{}, len(agentNames)+len(table.Module.AgentWorkflows))
	for name := range agentNames {
		availableNames[name] = struct{}{}
	}
	for i, workflow := range table.Module.AgentWorkflows {
		//检查 Workflow 类型是否合法
		switch workflow.Type {
		case model.WorkflowTypeLoop,
			model.WorkflowTypeParallel,
			model.WorkflowTypeSequential,
			model.WorkflowTypeDrawIORepair:
		default:
			return fmt.Errorf(
				"%s: module.agent-workflows[%d].type is invalid: %s",
				prefix,
				i,
				workflow.Type,
			)
		}
		//检查 Workflow 名称
		workflowName := strings.TrimSpace(workflow.Name)
		if workflowName == "" {
			return fmt.Errorf(
				"%s: module.agent-workflows[%d].name is required",
				prefix,
				i,
			)
		}
		if _, exists := availableNames[workflowName]; exists {
			return fmt.Errorf(
				"%s: module.agent-workflows[%d].name conflicts with an existing agent or workflow: %s",
				prefix,
				i,
				workflowName,
			)
		}

		if workflow.Type == model.WorkflowTypeDrawIORepair {
			if err := validateDrawIORepairWorkflow(prefix, i, workflow, agentNames); err != nil {
				return err
			}
		} else {
			if workflow.MaxRepairs != nil || workflow.Roles != (model.DrawIORepairRoles{}) {
				return fmt.Errorf(
					"%s: module.agent-workflows[%d] can only use roles/max-repairs with type %s",
					prefix,
					i,
					model.WorkflowTypeDrawIORepair,
				)
			}
			for subIndex, reference := range workflow.SubAgents {
				if _, exists := availableNames[strings.TrimSpace(reference)]; !exists {
					return fmt.Errorf(
						"%s: module.agent-workflows[%d].sub-agents[%d] references unknown or not-yet-built agent %q",
						prefix,
						i,
						subIndex,
						reference,
					)
				}
			}
		}

		availableNames[workflowName] = struct{}{}
	}

	if _, exists := availableNames[strings.TrimSpace(table.Module.Runner.AgentName)]; !exists {
		return fmt.Errorf(
			"%s: module.runner.agent-name references unknown agent or workflow %q",
			prefix,
			table.Module.Runner.AgentName,
		)
	}

	for i, tool := range table.Module.ChatModel.ToolMCPList {
		if err := validateMCPEntry(prefix, i, tool); err != nil {
			return err
		}
	}

	return nil
}

// validateDrawIORepairWorkflow 在启动期检查四角色和预算，避免请求执行到一半才发现装配错误。
func validateDrawIORepairWorkflow(
	prefix string,
	index int,
	workflow model.AgentWorkflowConfig,
	agentNames map[string]struct{},
) error {
	fieldPrefix := fmt.Sprintf("%s: module.agent-workflows[%d]", prefix, index)
	if len(workflow.SubAgents) != 0 {
		return fmt.Errorf("%s cannot define sub-agents for type %s; use roles", fieldPrefix, workflow.Type)
	}
	if workflow.MaxIterations != 0 {
		return fmt.Errorf("%s cannot define max-iterations for type %s; use max-repairs", fieldPrefix, workflow.Type)
	}
	if workflow.MaxRepairs == nil {
		return fmt.Errorf("%s.max-repairs is required after defaults are applied", fieldPrefix)
	}
	if *workflow.MaxRepairs < 0 || *workflow.MaxRepairs > diagramworkflow.MaxSupportedRepairs {
		return fmt.Errorf(
			"%s.max-repairs must be between 0 and %d",
			fieldPrefix,
			diagramworkflow.MaxSupportedRepairs,
		)
	}

	roleValues := []struct {
		field string
		name  string
	}{
		{field: "analyst", name: workflow.Roles.Analyst},
		{field: "drawer", name: workflow.Roles.Drawer},
		{field: "reviewer", name: workflow.Roles.Reviewer},
		{field: "repairer", name: workflow.Roles.Repairer},
	}
	seen := make(map[string]string, len(roleValues))
	for _, role := range roleValues {
		name := strings.TrimSpace(role.name)
		if name == "" {
			return fmt.Errorf("%s.roles.%s is required", fieldPrefix, role.field)
		}
		if _, exists := agentNames[name]; !exists {
			return fmt.Errorf("%s.roles.%s references unknown agent %q", fieldPrefix, role.field, role.name)
		}
		if previousRole, exists := seen[name]; exists {
			return fmt.Errorf(
				"%s.roles.%s duplicates roles.%s agent %q",
				fieldPrefix,
				role.field,
				previousRole,
				name,
			)
		}
		seen[name] = role.field
	}
	return nil
}

func validateMCPEntry(
	prefix string,
	index int,
	tool model.ToolMCPConfig,
) error {
	fieldPrefix := fmt.Sprintf(
		"%s: module.chat-model.tool-mcp-list[%d]",
		prefix,
		index,
	)
	//mcp通信方式必须也只能配置local、sse、stdio其中的一个
	count := 0

	if tool.Local != nil {
		count++
	}

	if tool.SSE != nil {
		count++
	}

	if tool.Stdio != nil {
		count++
	}

	switch count {
	case 0:
		return fmt.Errorf(
			"%s must define exactly one of local, sse, or stdio",
			fieldPrefix,
		)
	case 1:
	default:
		return fmt.Errorf(
			"%s cannot define multiple transport types",
			fieldPrefix,
		)
	}

	if tool.Local != nil {
		//如果存在的是local，则判断他的相关信息
		if strings.TrimSpace(tool.Local.Name) == "" {
			return fmt.Errorf(
				"%s.local.name is required",
				fieldPrefix,
			)
		}
	}

	if tool.SSE != nil {
		//如果存在的是sse，则判断他的相关信息

		if strings.TrimSpace(tool.SSE.Name) == "" {
			return fmt.Errorf(
				"%s.sse.name is required",
				fieldPrefix,
			)
		}
		if strings.TrimSpace(tool.SSE.BaseURI) == "" {
			return fmt.Errorf(
				"%s.sse.base-uri is required",
				fieldPrefix,
			)
		}
		if tool.SSE.RequestTimeout < 0 {
			return fmt.Errorf(
				"%s.sse.request-timeout must be positive",
				fieldPrefix,
			)
		}
	}

	if tool.Stdio != nil {
		//如果存在的是stdio，则判断他的相关信息

		if strings.TrimSpace(tool.Stdio.Name) == "" {
			return fmt.Errorf(
				"%s.stdio.name is required",
				fieldPrefix,
			)
		}
		if strings.TrimSpace(
			tool.Stdio.ServerParameters.Command,
		) == "" {
			return fmt.Errorf(
				"%s.stdio.server-parameters.command is required",
				fieldPrefix,
			)
		}
		if tool.Stdio.RequestTimeout < 0 {
			return fmt.Errorf(
				"%s.stdio.request-timeout must be positive",
				fieldPrefix,
			)
		}
	}

	return nil
}
