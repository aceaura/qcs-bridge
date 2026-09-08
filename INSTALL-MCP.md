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

```bash
cd qcs-bridge/infra
AWS_REGION=<你的region> bash create-queues.sh
```

记下输出的 3 个队列 URL（`qcs-commands.fifo` / `qcs-results.fifo` / `qcs-heartbeat.fifo`）。

## 2. 授权（一次）

编辑 `infra/iam-policy-qcs-bridge.json`，替换 `REGION` 和 `ACCOUNT_ID`，
然后把它作为 inline policy 挂到两个身份上：

- 你本地使用的 IAM 身份（qcs-mcp 只需 SQS 权限，**不需要 EKS 权限**）
- 你登录控制台（即 CloudShell）的身份

## 3. 分发共享密钥（两端各一次）

```bash
openssl rand -hex 32    # 生成一个密钥，例如 a1b2c3...
```

- **本地**：新建文件 `%USERPROFILE%\.qcs-secret`，内容就是这一行密钥
- **CloudShell**：打开 CloudShell 执行
  ```bash
  echo '<同一密钥>' > ~/.qcs-secret && chmod 600 ~/.qcs-secret
  ```

## 4. 在 CloudShell 启动 agent

CloudShell 控制台菜单 **Actions → Upload file**，上传 `bin/qcs-agent` 和 `infra/restart-qcs.sh`，然后：

```bash
mv ~/cloudshell_upload/qcs-agent ~/qcs-agent 2>/dev/null || mv ~/qcs-agent.upload ~/qcs-agent 2>/dev/null || true
# 如果上传目录不同，直接把文件放到 ~/ 即可
chmod +x ~/qcs-agent
cp ~/cloudshell_upload/restart-qcs.sh ~/restart-qcs.sh 2>/dev/null || true
chmod +x ~/restart-qcs.sh
~/restart-qcs.sh
```

看到 `qcs-agent started (pid ...)` 即成功。可用 `tail -f ~/qcs-agent.log` 观察。

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
- 返回 `OFFLINE` → CloudShell 的 VM 被回收了，重开 CloudShell 跑 `~/restart-qcs.sh`

## 运维须知

- CloudShell 空闲约 20-30 分钟回收 VM，agent 随之退出。重开 CloudShell → `~/restart-qcs.sh` 即可恢复。CloudShell 的 home 目录持久，`~/qcs-agent` 和密钥不会丢。
- 审计：CloudShell 内 `~/qcs-audit.log` 记录每一条执行/拒绝的命令。
- agent 默认只读模式。确需放开时在 `~/restart-qcs.sh` 中给 `qcs-agent` 加 `--allow-all`（不推荐长期使用）。
