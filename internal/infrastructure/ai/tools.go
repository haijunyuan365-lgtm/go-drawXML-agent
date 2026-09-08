package ai

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"

	"gopkg.in/yaml.v3"
)

// ToolFactory 负责把 YAML 中的 MCP 配置构造成模型可见工具，并交给路由器注册。
type ToolFactory struct {
	router *MCPToolRouter
}

type SkillFactory struct{}

// MCPTool 保存 MCP Server 的传输配置；它本身只是一份配置型 Tool。
type MCPTool struct {
	ToolName       string
	TransportType  string
	BaseURI        string
	SSEEndpoint    string
	Command        string
	Args           []string
	Env            map[string]string
	RequestTimeout int
}

type SkillTool struct {
	ToolName     string
	SkillName    string
	Description  string
	Path         string
	ManifestPath string
}

// NewToolFactory 注入进程级共享 MCPToolRouter。
func NewToolFactory(router *MCPToolRouter) *ToolFactory {
	return &ToolFactory{router: router}
}

func NewSkillFactory() *SkillFactory {
	return &SkillFactory{}
}

// BuildTools 校验单条 MCP 配置，构造 Tool，并在 SSE 场景下展开远端真实工具名。
func (f *ToolFactory) BuildTools(_ context.Context, config model.ToolMCPConfig) ([]ports.Tool, error) {
	kind, err := validateMCPConfig(config)
	if err != nil {
		return nil, err
	}

	var (
		tools []ports.Tool
	)
	switch {
	case kind == "local":
		tools, err = buildLocalMCPTools(config.Local)
	case kind == "sse":
		tools, err = buildSSEMCPTools(config.SSE)
	case kind == "stdio":
		tools, err = buildStdioMCPTools(config.Stdio)
	case config.SSE != nil:
		tools, err = buildSSEMCPTools(config.SSE)
	case config.Stdio != nil:
		tools, err = buildStdioMCPTools(config.Stdio)
	default:
		return nil, fmt.Errorf("mcp tool config is empty")
	}
	if err != nil {
		return nil, err
	}
	if f.router != nil {
		expanded := make([]ports.Tool, 0, len(tools))
		for _, t := range tools {
			ts := f.router.RegisterAndExpand(t)
			if len(ts) == 0 {
				continue
			}
			expanded = append(expanded, ts...)
		}
		if len(expanded) > 0 {
			return expanded, nil
		}
	}
	return tools, nil
}

// Name 让 MCPTool 实现领域层 ports.Tool。
func (t MCPTool) Name() string {
	return t.ToolName
}

func (f *SkillFactory) BuildTools(_ context.Context, config model.ToolSkillsConfig) ([]ports.Tool, error) {
	skillType := strings.TrimSpace(config.Type)
	if skillType == "" {
		skillType = "directory"
	}
	if skillType != "directory" && skillType != "resource" {
		return nil, fmt.Errorf("unsupported skill type %q", config.Type)
	}

	rawPath := strings.TrimSpace(config.Path)
	if rawPath == "" {
		return nil, fmt.Errorf("skill path is required")
	}

	root, err := resolveSkillRoot(skillType, rawPath)
	if err != nil {
		return nil, err
	}
	manifests, err := findSkillManifests(root)
	if err != nil {
		return nil, err
	}
	if len(manifests) == 0 {
		return nil, fmt.Errorf("skill path %q has no SKILL.md files", root)
	}

	tools := make([]ports.Tool, 0, len(manifests))
	for _, manifest := range manifests {
		tool, err := loadSkillTool(root, manifest)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func (t SkillTool) Name() string {
	return t.ToolName
}

func resolveSkillRoot(skillType, rawPath string) (string, error) {
	if filepath.IsAbs(rawPath) {
		return existingPath(rawPath, rawPath)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve skill path %q: %w", rawPath, err)
	}

	candidates := skillPathCandidates(cwd, skillType, rawPath)
	for _, candidate := range candidates {
		if path, err := existingPath(candidate, rawPath); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("skill path %q cannot be resolved", rawPath)
}

func skillPathCandidates(cwd, skillType, rawPath string) []string {
	var candidates []string
	add := func(path string) {
		clean := filepath.Clean(path)
		for _, candidate := range candidates {
			if candidate == clean {
				return
			}
		}
		candidates = append(candidates, clean)
	}

	for dir := cwd; ; dir = filepath.Dir(dir) {
		add(filepath.Join(dir, rawPath))
		add(filepath.Join(dir, "configs", rawPath))
		add(filepath.Join(dir, "ai-agent-scaffold-go", rawPath))
		add(filepath.Join(dir, "ai-agent-scaffold-go", "configs", rawPath))
		if skillType == "resource" && strings.HasPrefix(rawPath, "agent"+string(filepath.Separator)) {
			add(filepath.Join(dir, "configs", rawPath))
			add(filepath.Join(dir, "ai-agent-scaffold-go", "configs", rawPath))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return candidates
}

func existingPath(candidate, original string) (string, error) {
	info, err := os.Stat(candidate)
	if err != nil {
		return "", fmt.Errorf("skill path %q cannot be resolved", original)
	}
	if !info.IsDir() && filepath.Base(candidate) != "SKILL.md" {
		return "", fmt.Errorf("skill path %q is not a directory or SKILL.md file", candidate)
	}
	return candidate, nil
}

func findSkillManifests(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{root}, nil
	}

	var manifests []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == "SKILL.md" {
			manifests = append(manifests, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan skill path %q: %w", root, err)
	}
	sort.Strings(manifests)
	return manifests, nil
}

func loadSkillTool(root, manifest string) (SkillTool, error) {
	data, err := os.ReadFile(manifest)
	if err != nil {
		return SkillTool{}, fmt.Errorf("read skill manifest %q: %w", manifest, err)
	}
	meta, err := parseSkillFrontMatter(string(data))
	if err != nil {
		return SkillTool{}, fmt.Errorf("parse skill manifest %q: %w", manifest, err)
	}

	skillDir := filepath.Dir(manifest)
	name := strings.TrimSpace(meta["name"])
	if name == "" {
		name = filepath.Base(skillDir)
	}
	description := strings.TrimSpace(meta["description"])

	return SkillTool{
		ToolName:     "skill_" + sanitizeToolName(name),
		SkillName:    name,
		Description:  description,
		Path:         skillDir,
		ManifestPath: manifest,
	}, nil
}

func sanitizeToolName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		out = "tool"
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func parseSkillFrontMatter(content string) (map[string]string, error) {
	result := make(map[string]string)
	if !strings.HasPrefix(content, "---") {
		return result, nil
	}

	rest := strings.TrimPrefix(content, "---")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return result, fmt.Errorf("front matter terminator is required")
	}

	frontMatter := rest[:end]
	if err := yaml.Unmarshal([]byte(frontMatter), &result); err != nil {
		return nil, err
	}
	return result, nil
}

// validateMCPConfig 保证一条配置只能选择 local、sse、stdio 中的一种传输。
func validateMCPConfig(config model.ToolMCPConfig) (string, error) {
	count := 0
	kind := ""
	if config.Local != nil {
		count++
		kind = "local"
	}
	if config.SSE != nil {
		count++
		kind = "sse"
	}
	if config.Stdio != nil {
		count++
		kind = "stdio"
	}

	switch count {
	case 0:
		return "", fmt.Errorf("mcp config must define exactly one of local, sse, or stdio")
	case 1:
		return kind, nil
	default:
		return "", fmt.Errorf("mcp config cannot define multiple transports in one entry")
	}
}

// buildLocalMCPTools 构造 Go 进程内预注册工具；当前源码仅示例 echoLocalTool。
func buildLocalMCPTools(config *model.LocalToolParameters) ([]ports.Tool, error) {
	var name = strings.TrimSpace(config.Name)
	if name == "" {
		return nil, fmt.Errorf("local mcp tool name is required")
	}

	switch name {
	case "echoLocalTool":
		return []ports.Tool{MCPTool{ToolName: name, TransportType: "local"}}, nil
	default:
		return nil, fmt.Errorf("local mcp tool %q is not registered in go", name)
	}
}

// buildSSEMCPTools 校验并标准化 SSE MCP 地址。
func buildSSEMCPTools(config *model.SSEServerParameters) ([]ports.Tool, error) {
	name := strings.TrimSpace(config.Name)
	if name == "" {
		return nil, fmt.Errorf("sse mcp tool name is required")
	}
	baseURI := strings.TrimSpace(config.BaseURI)
	if baseURI == "" {
		return nil, fmt.Errorf("sse mcp tool %q base-uri is required", name)
	}

	normalizedBaseURI, endpoint, err := normalizeSSETarget(baseURI, strings.TrimSpace(config.SSEEndpoint))
	if err != nil {
		return nil, fmt.Errorf("sse mcp tool %q: %w", name, err)
	}
	timeout := normalizeTimeout(config.RequestTimeout)

	return []ports.Tool{MCPTool{
		ToolName:       name,
		TransportType:  "sse",
		BaseURI:        normalizedBaseURI,
		SSEEndpoint:    endpoint,
		RequestTimeout: timeout,
	}}, nil
}

// buildStdioMCPTools 保存 stdio 启动参数；当前运行时尚未真正执行 stdio。
func buildStdioMCPTools(config *model.StdioServerParameters) ([]ports.Tool, error) {
	name := strings.TrimSpace(config.Name)
	if name == "" {
		return nil, fmt.Errorf("stdio mcp tool name is required")
	}
	command := strings.TrimSpace(config.ServerParameters.Command)
	if command == "" {
		return nil, fmt.Errorf("stdio mcp tool %q command is required", name)
	}
	timeout := normalizeTimeout(config.RequestTimeout)

	return []ports.Tool{MCPTool{
		ToolName:       name,
		TransportType:  "stdio",
		Command:        command,
		Args:           append([]string(nil), config.ServerParameters.Args...),
		Env:            cloneEnv(config.ServerParameters.Env),
		RequestTimeout: timeout,
	}}, nil
}

// normalizeSSETarget 把 base-uri 中的路径/查询参数与 sse-endpoint 合并。
func normalizeSSETarget(baseURI, endpoint string) (string, string, error) {
	parsed, err := url.Parse(baseURI)
	if err != nil {
		return "", "", fmt.Errorf("invalid base-uri: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("base-uri must include scheme and host")
	}

	host := strings.TrimRight(parsed.Scheme+"://"+parsed.Host, "/")
	basePath := parsed.RawPath
	if basePath == "" {
		basePath = parsed.EscapedPath()
	}
	baseQuery := parsed.RawQuery

	if endpoint == "" {
		if basePath == "" || basePath == "/" {
			return host, "/sse", nil
		}
		merged := basePath
		if baseQuery != "" {
			merged += "?" + baseQuery
		}
		return host, normalizeEndpoint(merged), nil
	}

	endpointPath, endpointQuery := splitPathQuery(endpoint)
	mergedPath := joinPaths(basePath, endpointPath)
	mergedQuery := mergeQueries(baseQuery, endpointQuery)
	if mergedQuery != "" {
		mergedPath += "?" + mergedQuery
	}
	return host, normalizeEndpoint(mergedPath), nil
}

// splitPathQuery 拆分路径与查询字符串。
func splitPathQuery(raw string) (string, string) {
	if idx := strings.Index(raw, "?"); idx >= 0 {
		return raw[:idx], raw[idx+1:]
	}
	return raw, ""
}

// joinPaths 合并基础路径和 endpoint 路径。
func joinPaths(base, extra string) string {
	if extra == "" {
		if base == "" {
			return "/"
		}
		return base
	}
	if strings.HasPrefix(extra, "/") {
		return extra
	}
	if base == "" {
		return "/" + extra
	}
	if strings.HasSuffix(base, "/") {
		return base + extra
	}
	return base + "/" + extra
}

// mergeQueries 合并 base-uri 与 endpoint 各自携带的查询参数。
func mergeQueries(base, extra string) string {
	switch {
	case base == "" && extra == "":
		return ""
	case base == "":
		return extra
	case extra == "":
		return base
	default:
		return base + "&" + extra
	}
}

// normalizeEndpoint 确保 endpoint 非空且以 / 开头。
func normalizeEndpoint(endpoint string) string {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return "/sse"
	}
	if strings.HasPrefix(trimmed, "/") {
		return trimmed
	}
	return "/" + trimmed
}

// normalizeTimeout 为 MCP 请求超时提供默认值 300000ms。
func normalizeTimeout(timeout int) int {
	if timeout > 0 {
		return timeout
	}
	return 300000
}

// cloneEnv 防止调用方后续修改原 map，影响已构建的 stdio 配置。
func cloneEnv(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

// maskSecretPath 是源码保留的敏感查询参数脱敏辅助函数。
func maskSecretPath(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := parsed.Query()
	changed := false
	for key := range query {
		upper := strings.ToUpper(key)
		if strings.Contains(upper, "KEY") || strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") {
			query.Set(key, "${"+strings.ToUpper(strings.ReplaceAll(key, "-", "_"))+"}")
			changed = true
		}
	}
	if !changed {
		return raw
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// mcpTimeoutString 将毫秒超时标准化后转成字符串。
func mcpTimeoutString(timeout int) string {
	return strconv.Itoa(normalizeTimeout(timeout))
}
