# qcs-bridge — Qoder ⇄ AWS CloudShell 通道

让 Qoder 直接以 CloudShell 的 IAM 身份和网络位置执行检测命令。

```
Qoder ──MCP──> qcs-mcp (本地) ──SQS──> qcs-agent (CloudShell) ──bash──> 线上环境
                 签名+发命令        长轮询      验签+执行+回传
```

**模式说明**：出站长轮询 agent，与 SSM Agent / ECS Agent / GitHub Actions runner 同架构。
CloudShell 无入站能力、无远程执行 API，这是唯一不需要公网入口的标准做法。

## 两种安装模式（二选一）

- **MCP 模式**：Qoder 通过 MCP 工具直接调用 → 按 [INSTALL-MCP.md](INSTALL-MCP.md) 安装
- **CLI + Skill 模式**：Qoder 通过 Bash 调 `qcs` 命令行，Skill 文件教会它用法 → 按 [INSTALL-CLI.md](INSTALL-CLI.md) 安装

两个指引文件各自包含全部步骤（队列、授权、密钥、agent、客户端），不需要交叉参考。

## 组件

| 文件 | 运行位置 | 说明 |
|---|---|---|
| `cmd/qcs-agent` | CloudShell (linux/amd64) | 轮询命令队列、验签、只读白名单、执行、回传结果、60s 心跳、审计日志 |
| `cmd/qcs-mcp` | 本地 Windows | MCP server 模式，暴露 `cloudshell_exec` / `cloudshell_status` 工具 |
| `cmd/qcs` | 本地 Windows | CLI 模式，`qcs exec "cmd"` / `qcs status`，退出码透传远端 |
| `internal/client` | 共享（本地） | qcs-mcp 与 qcs 共用的桥接客户端 |
| `internal/proto` | 共享 | 消息格式、HMAC-SHA256 签名/验签、只读白名单、输出截断 |
| `skills/qcs-cloudshell/SKILL.md` | Qoder 技能目录 | CLI 模式下教 Qoder 何时、如何用 `qcs` |
| `infra/` | 一次性部署 | 建队列脚本、最小 IAM policy、agent 重启脚本 |

## 安全设计

- **HMAC-SHA256 签名**每条命令/结果/心跳，密钥不进队列；时间戳偏差 >5 分钟拒收（防重放）
- **只读白名单**（agent 默认开启）：`kubectl get/describe/logs/top`、`aws * describe/list/get/...`、`aws s3 ls`、基础诊断命令；含 `; & | < > $ \` ( ) \` 换行等 shell 元字符的命令一律拒绝（防命令链绕过）
- 队列开 SQS 托管 SSE，消息保留 1 小时
- agent 审计日志：`~/qcs-audit.log`（时间、命令、退出码、拒绝原因）
- 队列 IAM policy 只授权两个指定身份（`infra/iam-policy-qcs-bridge.json`）

## 部署

完整步骤见 [INSTALL-MCP.md](INSTALL-MCP.md) 或 [INSTALL-CLI.md](INSTALL-CLI.md)（各自自包含，选一个跟随即可）。

## 已知限制

- **CloudShell 空闲 ~20-30 分钟回收 VM，agent 会死**——重开 CloudShell 跑 `~/restart-qcs.sh` 即可。`cloudshell_status` 会报告心跳新鲜度（>3 分钟判 STALE）。
- 单次输出 ≤ ~200KB（超出截断并标注 `[truncated]`）
- 端到端延迟 ~1-3 秒（SQS 长轮询）
- agent 单并发：一次只执行一条命令

## 开发

```bash
cd qcs-bridge
go test ./...                                  # 单元测试（签名/白名单/截断）
go build -o bin/qcs-mcp.exe ./cmd/qcs-mcp      # 本地 MCP
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -o bin/qcs-agent ./cmd/qcs-agent    # CloudShell agent
```
