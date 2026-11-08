# compress 高危告警与依赖图同步

## 范围与观察

[Dependabot #1](https://github.com/hopecommon/sii-link/security/dependabot/1)
对应 [GHSA-259r-337f-4rfw](https://github.com/klauspost/compress/security/advisories/GHSA-259r-337f-4rfw)
（CVE-2026-63209）。上游公告列出的受影响版本为 `<=v1.18.6`，
最低修复版本为 `v1.18.7`，问题涉及读取不可信 S2 字典后编码时的整数溢出。

2026-11-09 核查时，告警仍记录 `go.mod = 1.18.6`，GitHub SBOM 同样显示
`v1.18.6`；但主线早已在 `04cf5e8` 升级依赖。现场核对的四层证据为：

- GitHub `main` 的 `go.mod`：`github.com/klauspost/compress v1.20.3`。
- 本地 `go list -m -json github.com/klauspost/compress`：`v1.20.3`。
- 已安装 `sii-link` 的 `go version -m`：`v1.20.3`。
- 当前模块的 `s2.NewDict` 在转换为 int 前，先以 uint64 检查字典索引范围。

因此当前源代码和已安装二进制已含修复，剩余问题是 GitHub 依赖图与实际构建
不一致。没有为消除告警再次升级或降级依赖，也没有把告警手动忽略。

## 行为验证

使用临时 Go 程序，将公告中的大于 MaxInt64 的 repeat 值
`12687765596934209639` 编码为 uvarint，附加 16 字节字典并调用当前版本
`s2.NewDict`。程序检查返回值必须为 nil，实际输出：

```text
PASS: overflow dictionary rejected
```

`go mod why -m github.com/klauspost/compress` 显示依赖链为
`client/easyconnect -> refraction-networking/utls -> compress/zstd`。
`go list -deps .` 没有 `compress/s2`；该结论只覆盖本机的当前构建目标。
未在运行中的 VPN 进程注入恶意数据，临时探针执行后自动清理。

## 修正与验收

CI 增加 GitHub 官方 `actions/go-dependency-submission@v2`，在 main 的
可信事件中提交 Linux CI 的项目包 `./...` 实际解析结果，并关联原始 `go.mod`。
该目标没有启用 `go list -test`。该快照只代表这一
构建目标，本机 macOS 二进制版本另行核对。仅该 job
授予提交依赖图需要的 `contents: write`；PR 不运行它，其余 job 权限不扩大。
运行时依赖与 VPN 服务不变。

GitHub 官方说明：用户提交的依赖快照优先于静态扫描与自动解析，提交后会重建
依赖图并更新 Dependabot 判断。参考
[依赖提交 API](https://docs.github.com/en/rest/dependency-graph/dependency-submission)
与 [官方 Action](https://github.com/actions/go-dependency-submission)。

远端完成标准：dependency-graph job 成功，SBOM 中 compress 为 `v1.20.3`，
告警 #1 被 GitHub 判定为 fixed，开放告警为空。远端最终结果在本文件的提交后
验收中记录；在观察到这些结果前，不把 CI 配置提交本身当作告警已关闭。
