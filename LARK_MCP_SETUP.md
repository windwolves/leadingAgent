# 飞书 MCP 集成说明

## 配置信息
- App ID: `cli_a93b2641e038dced`
- App Secret: `lElxdEC0OtHK2FAEpsGa3f1Mt1m3KLso`
- 已全局安装 lark-mcp: ✅
- 登录状态: ✅ Successfully logged in

## 如何在 Trae 中配置

在 Trae 的 MCP 配置中添加以下配置：

```json
{
  "mcpServers": {
    "lark-mcp": {
      "command": "/bin/bash",
      "args": [
        "-c",
        "export PATH=/opt/homebrew/bin:$PATH && export NODE_TLS_REJECT_UNAUTHORIZED=0 && node /opt/homebrew/lib/node_modules/@larksuiteoapi/lark-mcp/dist/cli.js mcp -a cli_a93b2641e038dced -s lElxdEC0OtHK2FAEpsGa3f1Mt1m3KLso --oauth"
      ]
    }
  }
}
```

## 登录步骤

1. 首先运行登录命令获取授权：
```bash
export PATH=/opt/homebrew/bin:$PATH
export NODE_TLS_REJECT_UNAUTHORIZED=0
node /opt/homebrew/lib/node_modules/@larksuiteoapi/lark-mcp/dist/cli.js login -a cli_a93b2641e038dced -s lElxdEC0OtHK2FAEpsGa3f1Mt1m3KLso
```

2. 根据提示访问授权 URL 完成飞书账号授权
3. 确保在飞书应用的安全设置中已配置重定向 URL：`http://localhost:3000/callback`

## 支持的飞书能力

- 云文档操作
- 多维表格操作
- 日历管理
- 消息发送
- 更多功能参考飞书开放平台文档

## 其他有用命令

查看帮助：
```bash
export PATH=/opt/homebrew/bin:$PATH
export NODE_TLS_REJECT_UNAUTHORIZED=0
node /opt/homebrew/lib/node_modules/@larksuiteoapi/lark-mcp/dist/cli.js --help
```

查看当前会话：
```bash
export PATH=/opt/homebrew/bin:$PATH
export NODE_TLS_REJECT_UNAUTHORIZED=0
node /opt/homebrew/lib/node_modules/@larksuiteoapi/lark-mcp/dist/cli.js whoami
```

登出：
```bash
export PATH=/opt/homebrew/bin:$PATH
export NODE_TLS_REJECT_UNAUTHORIZED=0
node /opt/homebrew/lib/node_modules/@larksuiteoapi/lark-mcp/dist/cli.js logout
```

## 注意事项

由于网络环境限制，需要设置 `NODE_TLS_REJECT_UNAUTHORIZED=0` 来禁用 SSL 证书验证。
