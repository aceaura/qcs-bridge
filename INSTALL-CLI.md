# qcs-bridge 安装指引 — CLI + Skill 模式

一个文件装完。全程约 10 分钟。装完后 Qoder 通过 `qcs` 命令行操作 CloudShell，
Skill 文件让 Qoder 自动知道何时、如何使用它。

> 另一种模式（MCP server）见 `INSTALL-MCP.md`，二选一即可。

## 0. 前置

- 本地：AWS CLI 已配置好你的凭证（`aws sts get-caller-identity` 能通）
- 本目录已有编译产物：`bin/qcs.exe`（本地 CLI）和 `bin/qcs-agent`（CloudShell）。
  如需自行编译：
  ```bash
  cd qcs-bridge
  go build -ldflags="-s -w" -o bin/qcs.exe ./cmd/qcs
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/qcs-agent ./cmd/qcs-agent
  ```

## 1. 创建 SQS 队列（一次）

```bash
cd qcs-bridge/infra
AWS_REGION=<你的region> bash create-queues.sh
```

## 2. 授权（一次）

编辑 `infra/iam-policy-qcs-bridge.json`，替换 `REGION` 和 `ACCOUNT_ID`，
作为 inline policy 挂到两个身份：

- 你本地使用的 IAM 身份（qcs 只需 SQS 权限，**不需要 EKS 权限**）
- 你登录控制台（即 CloudShell）的身份

## 3. 分发共享密钥（本地一次，CloudShell 在第 4 步安装时交互输入）

```bash
openssl rand -hex 32    # 生成一个密钥
```

- **本地**：新建文件 `%USERPROFILE%\.qcs-secret`，内容就是这一行密钥
- **CloudShell**：下一步的安装脚本会提示你粘贴，自动写入 `~/.qcs-secret`

## 4. 在 CloudShell 安装并启动 agent

CloudShell 控制台 **Actions → Upload file**，上传 `bin/qcs-agent` 和 `infra/install-agent.sh` 到 `~/`，然后：

```bash
bash install-agent.sh        # 安装到 ~/.qcs/，途中粘贴共享密钥
qcs-start                    # 前台启动，所有收发交互实时显示在终端
```

- 以后任何时候敲 `qcs-start` 即可启动；后台模式：`qcs-start --background`，配合 `tail -f ~/qcs-agent.log`。
- 终端可见：`>>> RECV` 收到的命令、`<<< RESULT` 完整 stdout/stderr、每分钟心跳；审计流水在 `~/qcs-audit.log`。

## 5. 安装 CLI + Skill（本地）

**5a. 配置环境变量**（Windows，管理员不需要，普通 cmd 执行；设完重开终端生效）：

```cmd
setx QCS_CMD_QUEUE qcs-commands.fifo
setx QCS_RESULT_QUEUE qcs-results.fifo
setx QCS_HEARTBEAT_QUEUE qcs-heartbeat.fifo
setx AWS_REGION <你的region>
```

**5b. 装 CLI 到 PATH**（任选）：把 `bin/qcs.exe` 拷到已在 PATH 的目录，或：

```cmd
setx PATH "%PATH%;W:\QoderCN\qcs-bridge\bin"
```

**5c. 装 Skill**：把 `skills/qcs-cloudshell/SKILL.md` 复制到 Qoder 用户技能目录：

```cmd
mkdir "%USERPROFILE%\.qoder-cn\skills\qcs-cloudshell" 2>nul
copy skills\qcs-cloudshell\SKILL.md "%USERPROFILE%\.qoder-cn\skills\qcs-cloudshell\SKILL.md"
```

重开 Qoder 会话后，Skill 会在你要求检测线上环境时自动生效。

## 6. 验证

新开一个终端：

```bash
qcs status
```

- 输出 `ONLINE` → 完成。然后在 Qoder 里说"检查 eks 集群状态"，Qoder 会自动调用 `qcs exec`。
- `OFFLINE` → CloudShell 的 VM 被回收了，重开 CloudShell 运行 `qcs-start`。

## 运维须知

- CloudShell 空闲约 20-30 分钟回收 VM，agent 随之退出。重开 CloudShell → `qcs-start`。
- 审计：CloudShell 内 `~/qcs-audit.log`；完整交互输出在终端或 `~/qcs-agent.log`。
- agent 默认只读模式；CLI 的退出码就是远端命令退出码，方便脚本化（`qcs exec "..." || alert`）。
