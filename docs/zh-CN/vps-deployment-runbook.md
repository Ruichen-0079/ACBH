# ACBH VPS 部署运行手册 (v0.3.2)

## 推荐配置

- CPU: 2 核起步，推荐 2-4 核
- 内存: 2G 起步，推荐 4G+
- 磁盘: 40G+ SSD
- 网络: 固定公网 IPv4，带宽 5Mbps+ 足够中转

## 安全组 / 防火墙

必须开放：

- TCP 25565：玩家 Minecraft 入口（0.0.0.0/0）
- TCP 6121：Coordinator API（建议限制为你的 Windows Host IP，或使用 SSH tunnel / WireGuard）
- TCP 22：SSH 管理

绝对不要开放：

- RCON 端口
- MC 裸端口（25565 以外）
- 任何内部管理端口

## 部署路径

推荐：

/opt/acbh

/opt/acbh/coordinator/dist/...

/opt/acbh/storage/   (artifact)

/opt/acbh/coordinator-state.json

/opt/acbh/.env

/opt/acbh/scripts/start-coordinator.sh

## systemd 服务

使用提供的 acbh-coordinator.service

```bash
sudo useradd -r -s /bin/false acbh || true
sudo mkdir -p /opt/acbh
sudo cp -r <bundle>/coordinator /opt/acbh/
sudo cp <bundle>/.env.example /opt/acbh/.env
sudo cp <bundle>/scripts/* /opt/acbh/scripts/
sudo cp <bundle>/systemd/acbh-coordinator.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now acbh-coordinator
```

.env 示例：

```
ACBH_RELAY_PUBLIC_HOST=0.0.0.0
ACBH_RELAY_PUBLIC_PORT=25565
PORT=6121
HOST=0.0.0.0
```

## 启动验证

- systemctl status acbh-coordinator
- ss -ltnp | grep 25565  (public relay)
- ss -ltnp | grep 6121

## Windows Host 连接

见 remote-vps-relay-sync.md

## 玩家连接

原版 Minecraft 多人游戏 -> 直接输入 VPS_PUBLIC_IP:25565

无需安装任何 mod 或代理。

## 故障排查

- 25565 连接失败：检查安全组、VPS 监听、是否有 current host + relay host 运行
- Coordinator 不可达：安全组 6121、防火墙、URL 拼写
- relay 启动被阻止：确认本机是 current host（GUI 状态显示）
- 非 current host 不能启动中转
- 日志中无 token 泄露

## 升级

停止服务，替换 coordinator/dist，重新加载 systemd，重启。
