package model

// command.go 描述的是：系统运行过程中要传递什么数据

type ArmoryCommand struct {
	Table AiAgentConfigTable
}

type ChatCommand struct {
	AgentID   string
	UserID    string
	SessionID string
	Message   string
	Content   ChatContent
}

type ChatContent struct {
	Texts       []TextPart
	Files       []FilePart
	InlineDatas []InlineDataPart
}

// 结构体里面只有一个数据项的大概率都是为了方便未来扩展
type TextPart struct {
	Message string
}

type FilePart struct {
	FileURI  string //→ 传文件地址
	MimeType string
}

type InlineDataPart struct {
	Bytes    []byte //→ 直接传文件内容
	MimeType string
}

type RegisteredAgent struct {
	AppName   string
	AgentID   string
	AgentName string
	AgentDesc string
	Runner    Runner
}

type Runner interface {
	CreateSession(userID string) (string, error)
	Run(userID, sessionID string, content ChatContent) ([]string, error)
	Stream(userID, sessionID string, content ChatContent) (<-chan string, <-chan error)
}
