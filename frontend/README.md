# Draw.io Agent Frontend

This directory contains the migrated Next.js draw.io intelligent drawing frontend for `ai-agent-scaffold-go`.

## Backend API

The frontend talks to the Go backend through:

```text
http://localhost:8091/api/v1
```

Override the API base URL with:

```bash
NEXT_PUBLIC_API_BASE_URL=http://localhost:8091/api/v1
```

Used endpoints:

- `GET /query_ai_agent_config_list`
- `POST /create_session`
- `POST /chat`

The Go chat endpoint returns:

```json
{
  "code": "0000",
  "info": "success",
  "data": {
    "content": "<agent output or draw.io xml>"
  }
}
```

The frontend treats `data.content` as draw.io XML when it looks like `mxfile` or `mxGraphModel`; otherwise it renders the content as a normal Agent message.

## Development

```bash
npm install
npm run dev
```

Open:

```text
http://localhost:3000
```

## Build

```bash
npm run build
```
