# qcs-bridge 安装指引 — MCP 模式

一个文件装完。全程约 10 分钟。装完后 Qoder 里直接说"检查 eks 集群状态"即可。

> 另一种模式（CLI + Skill）见 `INSTALL-CLI.md`，二选一即可。

## 0. 前置

- 本地：AWS CLI 已配置好你的凭证（`aws sts get-caller-identity` 能通）
- 本目录已有编译产物：`bin/qcs-mcp.exe`（本地）和 `bin/qcs-agent`（CloudShell）。
  如需自行编译：
  ```bash
  cd qcs-bridge
  go build -ldflags="-s -w" -o bin/qcs-mcp.exe ./cmd/qcs-mcp
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/qcs-agent ./cmd/qcs-agent
  ```

## 1. 创建 SQS 队列（一次）

本地 Git Bash 一条命令（先确认 `aws sts get-caller-identity` 能通）：

```bash
curl -fsSL https://raw.githubusercontent.com/aceaura/qcs-bridge/main/infra/create-queues.sh | AWS_REGION=<你的region> bash
```

（也可以本地 clone 后跑 `infra/create-queues.sh`，效果相同。）

记下输出的 3 个队列 URL（`qcs-commands.fifo` / `qcs-results.fifo` / `qcs-heartbeat.fifo`）。

## 2. 授权（一次）

编辑 `infra/iam-policy-qcs-bridge.json`，替换 `REGION` 和 `ACCOUNT_ID`，
然后把它作为 inline policy 挂到两个身份上：

- 你本地使用的 IAM 身份（qcs-mcp 只需 SQS 权限，**不需要 EKS 权限**）
- 你登录控制台（即 CloudShell）的身份

## 3. 分发共享密钥（本地一次，CloudShell 在第 4 步安装时交互输入）

```bash
openssl rand -hex 32    # 生成一个密钥，例如 a1b2c3...
```

- **本地**：新建文件 `%USERPROFILE%\.qcs-secret`，内容就是这一行密钥
- **CloudShell**：下一步的安装脚本会提示你粘贴，自动写入 `~/.qcs-secret`（权限 600）

## 4. 在 CloudShell 安装并启动 agent

打开 CloudShell，粘贴一条命令（自动下载二进制、装到 `~/.qcs/`、途中提示你粘贴共享密钥）：

```bash
curl -fsSL https://raw.githubusercontent.com/aceaura/qcs-bridge/main/infra/install-agent.sh | bash
```

装完启动：

```bash
qcs-start                    # 前台启动，所有收发交互实时显示在终端
```

- `qcs-start` 已装入 `~/.qcs/` 并加进 PATH，以后任何时候敲这一条命令即可启动。
- 想挂后台：`qcs-start --background`，然后用 `tail -f ~/qcs-agent.log` 看实时交互。
- 终端里会看到：每条收到的命令（`>>> RECV`）、完整输出（`<<< RESULT` + stdout/stderr 全文）、每分钟心跳。审计流水在 `~/qcs-audit.log`。

## 5. 配置 Qoder MCP

在 Qoder 的 MCP 配置中添加：

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
        "AWS_PROFILE": "<本地profile，如用默认凭证可删此行>"
      }
    }
  }
}
```

重启 Qoder 会话后生效。

## 6. 验证

在 Qoder 对话里说：**"用 cloudshell_status 看看桥在不在"**。

- 返回 `ONLINE` → 完成，可以直接派检测任务了
- 返回 `OFFLINE` → CloudShell 的 VM 被回收了，重开 CloudShell 运行 `qcs-start`

## 运维须知

- CloudShell 空闲约 20-30 分钟回收 VM，agent 随之退出。重开 CloudShell → `qcs-start`（或 `qcs-start --background`）即可恢复。CloudShell 的 home 目录持久，`~/.qcs/` 和密钥不会丢。
- 审计：CloudShell 内 `~/qcs-audit.log` 记录每一条执行/拒绝的命令；完整交互输出在终端或 `~/qcs-agent.log`。
- agent 默认只读模式。确需放开时编辑 `~/.qcs/qcs-start`，给 `qcs-agent` 加 `--allow-all`（不推荐长期使用）。
