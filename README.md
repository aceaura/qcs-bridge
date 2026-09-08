# qcs-bridge — Qoder ⇄ AWS CloudShell 通道

让 Qoder 直接以 CloudShell 的 IAM 身份和网络位置执行检测命令。

```
Qoder ──MCP/CLI──> 本地桥接端 ──SQS──> qcs-agent (CloudShell) ──bash──> 线上环境
                     签名+发命令      长轮询      验签+执行+回传
```

**模式说明**：出站长轮询 agent，与 SSM Agent / ECS Agent / GitHub Actions runner 同架构。
CloudShell 无入站能力、无远程执行 API，这是唯一不需要公网入口的标准做法。

## 安装

全程约 10 分钟。本地客户端两种模式二选一（见第 5 步）。

**1. 创建 SQS 队列**（本地 Git Bash，前提 `aws sts get-caller-identity` 能通）：

```bash
curl -fsSL https://raw.githubusercontent.com/aceaura/qcs-bridge/main/infra/create-queues.sh | AWS_REGION=<你的region> bash
```

**2. 授权**（一次）：把 `infra/iam-policy-qcs-bridge.json` 里的 `REGION`/`ACCOUNT_ID` 替换后，
作为 inline policy 挂到两个身份：本地 IAM 身份（只需 SQS 权限，不需要 EKS）和 CloudShell 身份。

**3. 共享密钥**（本地一次）：`openssl rand -hex 32` 生成，写入 `%USERPROFILE%\.qcs-secret`。
CloudShell 侧在第 4 步由安装脚本提示粘贴。

**4. CloudShell 安装 agent**（CloudShell 内一条命令，自动下载二进制、装到 `~/.qcs/`、配好 PATH）：

```bash
curl -fsSL https://raw.githubusercontent.com/aceaura/qcs-bridge/main/infra/install-agent.sh | bash
```

之后启动只需敲 `qcs-start`（前台，所有收发交互实时滚屏；`qcs-start --background` 挂后台，`tail -f ~/qcs-agent.log` 观察）。

**5. 本地客户端（二选一）**：

- **MCP 模式** —— Qoder MCP 配置添加：
  ```json
  {
    "mcpServers": {
      "qcs-bridge": {
        "command": "W:\\QoderCN\\qcs-bridge\\bin\\qcs-mcp.exe",
        "env": {
          "QCS_CMD_QUEUE": "qcs-commands.fifo",
          "QCS_RESULT_QUEUE": "qcs-results.fifo",
          "QCS_HEARTBEAT_QUEUE": "qcs-heartbeat.fifo",
          "AWS_REGION": "<你的region>",
          "AWS_PROFILE": "<本地profile，用默认凭证可删>"
        }
      }
    }
  }
  ```
- **CLI + Skill 模式** —— 设环境变量后装 CLI 和技能：
  ```cmd
  setx QCS_CMD_QUEUE qcs-commands.fifo
  setx QCS_RESULT_QUEUE qcs-results.fifo
  setx QCS_HEARTBEAT_QUEUE qcs-heartbeat.fifo
  setx AWS_REGION <你的region>
  setx PATH "%PATH%;W:\QoderCN\qcs-bridge\bin"
  mkdir "%USERPROFILE%\.qoder-cn\skills\cloudshell" 2>nul
  copy skills\cloudshell\SKILL.md "%USERPROFILE%\.qoder-cn\skills\cloudshell\SKILL.md"
  ```
  （设完重开终端/Qoder 会话生效。）

**6. 验证**：Qoder 里说"用 cloudshell_status / qcs status 看看桥在不在"，返回 `ONLINE` 即完成；
`OFFLINE` 说明 CloudShell 的 VM 被回收，重开 CloudShell 运行 `qcs-start`。

## 组件

| 文件 | 运行位置 | 说明 |
|---|---|---|
| `cmd/qcs-agent` | CloudShell (linux/amd64) | 轮询命令队列、验签、只读白名单、执行、回传结果、60s 心跳、审计日志 |
| `cmd/qcs-mcp` | 本地 Windows | MCP server 模式，暴露 `cloudshell_exec` / `cloudshell_status` 工具 |
| `cmd/qcs` | 本地 Windows | CLI 模式，`qcs exec "cmd"` / `qcs status`，退出码透传远端 |
| `internal/client` | 共享（本地） | qcs-mcp 与 qcs 共用的桥接客户端 |
| `internal/proto` | 共享 | 消息格式、HMAC-SHA256 签名/验签、只读白名单、输出截断 |
| `skills/cloudshell/SKILL.md` | Qoder 技能目录 | CLI 模式下教 Qoder 何时、如何用 `qcs`（`/cloudshell`） |
| `infra/` | 部署 | 建队列脚本、最小 IAM policy、CloudShell 安装脚本（装到 `~/.qcs/`，提供 `qcs-start`） |

## 安全设计

- **HMAC-SHA256 签名**每条命令/结果/心跳，密钥不进队列；时间戳偏差 >5 分钟拒收（防重放）
- **只读白名单**（agent 默认开启）：`kubectl get/describe/logs/top`、`aws * describe/list/get/...`、`aws s3 ls`、基础诊断命令；含 `; & | < > $ \` ( ) \` 换行等 shell 元字符的命令一律拒绝（防命令链绕过）
- 队列开 SQS 托管 SSE，消息保留 1 小时
- agent 审计日志：`~/qcs-audit.log`（时间、命令、退出码、拒绝原因）
- 队列 IAM policy 只授权两个指定身份（`infra/iam-policy-qcs-bridge.json`）

## 已知限制

- **CloudShell 空闲 ~20-30 分钟回收 VM，agent 会死**——重开 CloudShell 敲 `qcs-start` 即可。`cloudshell_status` / `qcs status` 会报告心跳新鲜度（>3 分钟判 STALE）。
- 单次输出 ≤ ~200KB（超出截断并标注 `[truncated]`）
- 端到端延迟 ~1-3 秒（SQS 长轮询）
- agent 单并发：一次只执行一条命令

## 开发

```bash
cd qcs-bridge
go test ./...                                  # 单元测试（签名/白名单/截断）
go build -o bin/qcs-mcp.exe ./cmd/qcs-mcp      # 本地 MCP
go build -o bin/qcs.exe ./cmd/qcs              # 本地 CLI
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -o bin/qcs-agent ./cmd/qcs-agent    # CloudShell agent
```
