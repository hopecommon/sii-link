# Docker 运行（兼容方式）

SII Link 的主要部署方式是原生二进制；Dockerfile 仅保留给需要隔离运行通用
EasyConnect/aTrust 配置的用户。SII 的 macOS Keychain、休眠事件和 LaunchAgent
集成只在原生 macOS 方案中可用。

先创建不含真实凭据的 `config.toml`，再从当前源码构建：

```bash
docker compose up -d --build
docker compose logs -f sii-link
```

默认 Compose 文件映射 SOCKS5 `1080` 和 HTTP `1081`。若使用 SII 正式端口
`8888`，需要同时修改 `config.toml` 中的 `http_bind` 和 Compose 端口映射。

不要把密码、CAS ticket 或 `client-data.json` 烘焙进镜像。需要持久化状态时，
把状态目录作为 `0600` 文件所在的数据卷挂载。官方发布不承诺提供容器镜像；
应从与当前版本标签一致的源码自行构建。
