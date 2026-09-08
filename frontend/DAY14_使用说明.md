# Day 14 使用说明

本目录来自 `ai-agent-scaffold-go-main` 的 `frontend/`，只增加了中文注释，没有改变业务逻辑。

## 推荐学习与复制顺序

1. `package.json`、`tsconfig.json`、`next.config.ts`
2. `src/types/api.ts`
3. `src/config/api-config.ts`
4. `src/utils/cookie.ts`
5. `src/api/agent.ts`
6. `src/app/layout.tsx`、`src/app/globals.css`
7. `src/app/login/page.tsx`
8. `src/app/page.tsx`

## 本地启动

```powershell
cd frontend
npm install
npm run dev
```

浏览器打开 `http://localhost:3000/login`，演示账号为 `admin/admin`。

Go 后端需要运行在：

```text
http://localhost:8091/api/v1
```

也可以在 PowerShell 中先设置：

```powershell
$env:NEXT_PUBLIC_API_BASE_URL="http://localhost:8091/api/v1"
npm run dev
```

## 最重要的两个 sessionId

- `Session.id`：浏览器本地生成，用于左侧会话列表和 localStorage。
- `Session.backendSessionId`：Go 后端 Runner 创建，用于 `/chat` 请求。

二者不能混为一谈。
