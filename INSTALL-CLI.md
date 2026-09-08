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

## 3. 分发共享密钥（两端各一次）

```bash
openssl rand -hex 32    # 生成一个密钥
```

- **本地**：新建文件 `%USERPROFILE%\.qcs-secret`，内容就是这一行密钥
- **CloudShell**：
  ```bash
  echo '<同一密钥>' > ~/.qcs-secret && chmod 600 ~/.qcs-secret
  ```

## 4. 在 CloudShell 启动 agent

CloudShell 控制台 **Actions → Upload file**，上传 `bin/qcs-agent` 和 `infra/restart-qcs.sh` 到 `~/`，然后：

```bash
chmod +x ~/qcs-agent ~/restart-qcs.sh
~/restart-qcs.sh
```

看到 `qcs-agent started (pid ...)` 即成功。

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
- `OFFLINE` → CloudShell 的 VM 被回收了，重开 CloudShell 跑 `~/restart-qcs.sh`。

## 运维须知

- CloudShell 空闲约 20-30 分钟回收 VM，agent 随之退出。重开 CloudShell → `~/restart-qcs.sh`。
- 审计：CloudShell 内 `~/qcs-audit.log`。
- agent 默认只读模式；CLI 的退出码就是远端命令退出码，方便脚本化（`qcs exec "..." || alert`）。
