---
name: cloudshell
description: 通过 qcs CLI 在 AWS CloudShell 环境里执行命令（以 CloudShell 的 IAM 身份和网络位置）。当用户要求检查线上 AWS、EKS、kubectl 资源，或需要在 CloudShell 里执行命令，或提到 CloudShell、qcs、qcs-bridge 时使用。
---

# cloudshell

本机装有 `qcs` CLI，它通过 SQS 桥接在 AWS CloudShell 内执行命令（CloudShell 侧由 qcs-agent 常驻进程执行）。

## 使用方式

1. **先检查 agent 是否在线**：

   ```bash
   qcs status
   ```

   输出 `ONLINE` 即可继续。`STALE` 只说明心跳停了，**不等于 agent 已死**（心跳线程挂掉时命令通道仍可正常工作）——此时先发一条 exec 验证，能返回就继续。若为 `OFFLINE`，或 `STALE` 下 exec 也超时，告知用户：CloudShell 的 VM 可能已被回收（空闲约 20-30 分钟自动回收），请重新打开 CloudShell 并运行 `qcs-start`（或后台模式 `qcs-start --background`），然后重试。**不要**反复重试 exec。

2. **执行命令**：

   ```bash
   qcs exec "kubectl get pods -A"
   qcs exec -timeout 60 "aws ec2 describe-instances --region us-east-1"
   ```

   - 命令的退出码就是 qcs 的退出码；非 0 时把 `[stderr]` 段一并分析给用户。
   - 输出末尾 `[exit_code=N]` 是远端退出码标记。

## 限制（务必遵守）

- 该通道当前以 `--allow-all` 运行：**任意命令都会被真实执行**，没有白名单拦截。命令以 CloudShell 的普通用户身份执行（该用户在 `sudo` 组内）。
- 只读查询可以直接执行。**变更类或破坏性操作（写入、删除、部署、改权限、改配置）必须先向用户说明影响并取得确认**，不要自行执行。
- agent 也可能被重启回只读白名单模式（不带 `--allow-all`）。此时不在白名单内的命令、以及含 `; & | < > $ \` ( )` 或换行的命令会被拒；被拒时**不要尝试绕过**，向用户说明并让其决定是手动执行还是放开模式。
- 单次输出上限 ~200KB，超出会被截断（输出中标注 `[truncated]`）；需要大输出时改用更窄的过滤条件重试。
- 端到端延迟约 1-3 秒，超时上限 300 秒。

## 故障排查

- `config error: set QCS_CMD_QUEUE...` → 配置文件缺失，指引用户按 qcs-bridge README 安装小节第 3b 步创建 `~/.qcs/config`。
- `exec error: timed out waiting for result` → agent 掉线或命令超时，先 `qcs status` 确认。
- `access denied` 类 AWS 报错 → 本地 IAM 身份缺 SQS 权限，参考 README 安装小节第 2 步。
