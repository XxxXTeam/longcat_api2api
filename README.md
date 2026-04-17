# longcat_api2api

一个极简的 Go 代理服务，把 LongCat 的接入方式统一包装成官方 OpenAI Chat Completions 接口。

当前对外暴露的接口：

- `GET /`
- `GET /dashboard`
- `GET /api/stats`
- `POST /v1/chat/completions`
- `GET /v1/models`
- `GET /healthz`

当前支持两种 LongCat 上游模式：

- `LONGCAT_UPSTREAM_FORMAT=openai`
  直接转发到 `https://api.longcat.chat/openai/v1/chat/completions`
- `LONGCAT_UPSTREAM_FORMAT=anthropic`
  转发到 `https://api.longcat.chat/anthropic/v1/messages`，并把请求和响应转换为 OpenAI 官方格式

## key.txt

程序默认读取当前目录下的 `key.txt`，每行一个 LongCat Key，例如：

```txt
lc-xxx-1
lc-xxx-2
lc-xxx-3
```

## 运行

```bash
go run .
```

或构建后运行：

```bash
go build .
./longcat_api2api
```

Windows:

```powershell
go build .
.\longcat_api2api.exe
```

## 配置文件

程序默认会读取当前目录下的 `config.json`。

也可以通过环境变量指定其他配置文件：

```powershell
$env:CONFIG_FILE = "custom-config.json"
.\longcat_api2api.exe
```

优先级：

- 环境变量覆盖配置文件
- 配置文件覆盖内置默认值

## API Key 鉴权

客户端鉴权支持两种请求头：

- `Authorization: Bearer <your-client-api-key>`
- `X-API-Key: <your-client-api-key>`

配置方式：

- 在 `config.json` 的 `api_keys` 数组里配置一个或多个客户端 key
- 或使用环境变量 `CLIENT_API_KEYS=key1,key2,key3`

行为说明：

- `api_keys` 为空时，不启用客户端鉴权
- `api_keys` 有值时，`/`、`/dashboard`、`/api/stats`、`/v1/models`、`/v1/chat/completions` 都需要鉴权
- `GET /healthz` 保持不鉴权，方便健康检查

## 环境变量

- `CONFIG_FILE`
  配置文件路径，默认 `config.json`
- `ADDR`
  服务监听地址，默认 `:8080`
- `PORT`
  仅在未设置 `ADDR` 时生效，例如 `8080`
- `KEY_FILE`
  key 文件路径，默认 `key.txt`
- `CLIENT_API_KEYS`
  客户端鉴权 key，多个值用英文逗号分隔
- `LONGCAT_UPSTREAM_FORMAT`
  上游格式，`openai` 或 `anthropic`，默认 `openai`
- `LONGCAT_OPENAI_BASE`
  OpenAI 风格上游地址，默认 `https://api.longcat.chat/openai`
- `LONGCAT_ANTHROPIC_BASE`
  Anthropic 风格上游地址，默认 `https://api.longcat.chat/anthropic`
- `HTTP_TIMEOUT`
  上游超时时间，支持秒数或 Go duration，默认 `120s`
- `KEY_COOLDOWN`
  key 进入冷却后的时长，默认 `90s`
- `DATA_FILE`
  本地统计数据文件，默认 `data/stats.json`
- `AUTO_SAVE_INTERVAL`
  本地数据自动保存周期，默认 `5s`

## WebUI 大屏

浏览器打开：

```txt
http://127.0.0.1:8080/
```

或者：

```txt
http://127.0.0.1:8080/dashboard
```

可视化展示：

- 总 Key、活跃、冷却中、禁用
- RPM、总请求、总输入 Token、总输出 Token
- 成功请求、失败请求、成功率、运行时长
- 模型调用热度
- HTTP 状态分布
- 最近实时日志

## 数据本地化

统计数据会自动持久化到本地 `JSON` 文件中，默认路径为：

```txt
data/stats.json
```

持久化内容包括：

- Key 状态
- 总请求数、成功数、失败数
- 总输入 Token、总输出 Token
- 模型调用统计
- HTTP 状态码统计
- 最近日志

## 调用示例

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "LongCat-Flash-Chat",
    "messages": [
      {"role": "system", "content": "你是一个助手"},
      {"role": "user", "content": "你好"}
    ]
  }'
```

流式：

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "LongCat-Flash-Chat",
    "stream": true,
    "messages": [
      {"role": "user", "content": "给我一句自我介绍"}
    ]
  }'
```

## 已知边界

- 当前未实现 `tool_calls` 请求体转换，只做基础 finish reason 映射
- 当前未实现 Responses API，只实现 `chat.completions`
