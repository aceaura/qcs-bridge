---
name: qcs-cloudshell
description: 通过 qcs CLI 在 AWS CloudShell 环境里执行只读检测命令（以 CloudShell 的 IAM 身份和网络位置）。当用户要求检查/巡检线上 AWS、EKS、kubectl 资源，或提到 CloudShell、qcs、qcs-bridge 时使用。
---

# qcs-cloudshell

本机装有 `qcs` CLI，它通过 SQS 桥接在 AWS CloudShell 内执行命令（CloudShell 侧由 qcs-agent 常驻进程执行）。

## 使用方式

1. **先检查 agent 是否在线**：

   ```bash
   qcs status
   ```

   输出 `ONLINE` 才能继续。若显示 `OFFLINE` 或 `STALE`，告知用户：CloudShell 的 VM 已被回收（空闲约 20-30 分钟自动回收），请重新打开 CloudShell 并运行 `~/restart-qcs.sh`，然后重试。**不要**反复重试 exec。

2. **执行检测命令**：

   ```bash
   qcs exec "kubectl get pods -A"
   qcs exec -timeout 60 "aws ec2 describe-instances --region us-east-1"
   ```

   - 命令的退出码就是 qcs 的退出码；非 0 时把 `[stderr]` 段一并分析给用户。
   - 输出末尾 `[exit_code=N]` 是远端退出码标记。

## 限制（务必遵守）

- agent 默认**只读白名单**模式：只允许 `kubectl get/describe/logs/top`、`aws * describe/list/get/ls/query/...`、基础诊断命令（ping/dig/df/cat 等）。含 `; & | < > $ \` ( )` 或换行的命令会被拒。被拒时**不要尝试绕过**——向用户说明该命令不在只读白名单内，需要用户在 CloudShell 里手动执行，或明确授权后重启 agent 时加 `--allow-all`。
- 单次输出上限 ~200KB，超出会被截断（输出中标注 `[truncated]`）；需要大输出时改用更窄的过滤条件重试。
- 端到端延迟约 1-3 秒，超时上限 300 秒。
- 该通道用于**检测/巡检**，不要用它做任何变更类操作。

## 故障排查

- `config error: set QCS_CMD_QUEUE...` → 环境变量未配置，指引用户按 INSTALL-CLI.md 配置。
- `exec error: timed out waiting for result` → agent 掉线或命令超时，先 `qcs status` 确认。
- `access denied` 类 AWS 报错 → 本地 IAM 身份缺 SQS 权限，参考 INSTALL-CLI.md 第 2 步。
