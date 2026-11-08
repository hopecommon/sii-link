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

## 最终结果

变更提交 `9f215c4` 的
[远端 CI](https://github.com/hopecommon/sii-link/actions/runs/42506429100)
已完成，结论为 success：测试、静态检查、依赖图提交及五个平台构建均成功。
此次 main 推送没有发布 release。

依赖图提交的
[job 128809956629](https://github.com/hopecommon/sii-link/actions/runs/42506429100/job/128809956629)
成功后再次通过 GitHub API 核对：

```text
SBOM github.com/klauspost/compress: v1.20.3
Dependabot alert #1 state: fixed
fixed_at: 2026-11-08T17:44:22Z
open Dependabot alerts: 0
```

本地 `actionlint v1.7.12` 与暂存改动的 `git diff --check` 通过。
审查确认本次仅改变依赖图同步与证据文档，未改动 Go 运行时依赖、VPN 登录或
代理配置。没有阻断项。当前告警已经由 GitHub 根据实际依赖关闭，未手动 dismiss。
后续验收文档提交使用 `[skip ci]`，避免为纯文档重复构建；上述行为与权限检查
对应包含工作流改动的提交，不以跳过的工作流代替验证。
