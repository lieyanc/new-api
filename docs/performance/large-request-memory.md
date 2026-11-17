# v0.13.2 大请求内存优化及上游修复核查

核查日期：2026-11-17。

本分支基于上游 `v0.13.2`，基线提交 `bee339d279ccecbf8c8a89e14ddbbd902f78bd5d`，只合入 RawMessage 批量深拷贝、Responses 序列化前过滤及相关测试/基准；没有合入上游 1.0 的其他代码。

## 本分支改动

- `common.DeepCopy` 为 `json.RawMessage` 使用 `bytes.Clone` 类型转换器，避免 copier v0.4.0 按字节反射；保留独立底层存储与重试隔离。
- `MarshalResponsesRequest` 对标准 Responses DTO 先过滤禁用字段，再序列化一次；其他适配器返回类型回退原通用过滤路径。
- 保持透传设置、参数覆盖顺序和显式零/false。过滤不修改调用方数据；原始 JSON 大整数也不再因过滤转换成 float64 而损失精度。
- 参数覆盖、入口解析、出站请求存储等仍为 v0.13.2 实现，不应把本分支视为已经包含上游全部内存优化。

## 验证与基准

代码与此前已通过测试的补丁逐文件逐字节一致，移入 Git 仓库后完成补丁应用检查与 `git diff --check`；没有重复运行未改变的测试或基准。

已通过：

```sh
go test -p 4 ./common ./relay/common -run 'Test(DeepCopy|MarshalResponsesRequest|RemoveDisabledFields)' -count=1
go test -p 4 ./relay -run '^$'
```

覆盖副本/重试隔离、嵌套 RawMessage、16 种过滤设置 × 5 种 stream_options 状态、DTO 值/指针、透传、非标准结果回退、nil、零/false、大整数和无效输入。第二条是 relay 包及依赖的编译检查，该包当时没有测试文件。

每请求 71,674,579 字节（68.354 MiB），25 段合成 base64 形状的图片字符串；没有真实用户图片，也不解码图片或发送上游请求。环境为 Go 1.26.8、Linux amd64、Xeon W-2235，GOMAXPROCS=2、GOGC=100、GOMEMLIMIT=off；每组独立进程、一次运行。

| 场景 | 原实现耗时 | 本补丁耗时 | 原实现峰值 RSS | 本补丁峰值 RSS | 原实现累计分配 | 本补丁累计分配 |
|---|---:|---:|---:|---:|---:|---:|
| 单请求 | 7.281 秒 | 0.822 秒 | 431.1 MiB | 293.7 MiB | 546.9 MiB | 341.8 MiB |
| 4 并发整批 | 14.832 秒 | 1.726 秒 | 1471.9 MiB | 1114.0 MiB | 2187.7 MiB | 1367.3 MiB |

基准只覆盖文件读取、DTO 解码、深拷贝、序列化及字段过滤，保留原始请求到流程结束。它不包括完整转发链路、token/图片处理、参数覆盖、上游等待、重试及其他服务；这些数值不是生产环境的安全容量，也不是新版上游的测量结果。

复现方式：

```sh
go test -c -o responses-bench.test ./relay/common
GOMAXPROCS=2 GOGC=100 GOMEMLIMIT=off /usr/bin/time -v ./responses-bench.test -test.run='^$' -test.bench='^BenchmarkResponsesBodyPipeline$/^optimized$/^concurrency=4$' -test.benchtime=1x -test.count=1
```

切换 legacy/optimized 和 concurrency=1/4，分别启动独立进程。

## 上游 v1.0 开始的相关改进

结论：上游已经处理了这次定位的两个主要问题，并且还有本补丁没有覆盖的改进。不能笼统地说所有 v1.0 版本都已修复，改动分多次合入。

版本归属通过 `git tag --contains <commit> --list 'v1.*' --sort=version:refname` 检查，并结合发布记录和实际源码，而非只依赖发布说明。

| 首次包含的版本 | 已核实改动 | 与本问题的关系 |
|---|---|---|
| [v1.0.0-rc.8](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.8) | [ae6a0336 / #5009](https://github.com/QuantumNous/new-api/commit/ae6a03364d1e73d46d8e8f03a6c1805e558584c6)：提取 model/group 使用 JSON 路径读取；没有禁用字段时直接返回原字节 | 减少无用解析；当确实需要删字段时，当时仍会整包编解码，属于部分缓解 |
| v1.0.0-rc.9 标签；[rc.10 发布说明](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.10)列出大媒体内存优化 | [fddf54cc](https://github.com/QuantumNous/new-api/commit/fddf54ccc5cf1c97c7a48c657c3cc204b3d9f68f)：出站 JSON 使用 BodyStorage；磁盘模式下从 reader 解码；参数覆盖改为在字节上操作 | 开启磁盘缓存且超过阈值时，出站正文可以落盘，降低等待上游期间的堆驻留；并减少部分中间副本。尚非全链路零拷贝 |
| [v1.0.0-rc.34](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.34) | [2cf177ac / #7221](https://github.com/QuantumNous/new-api/commit/2cf177ac487e62c627c7d423b65735ba2481ef4f)：RawMessage 用 bytes.Clone 批量深拷贝 | 与本补丁第一项的核心实现相同，直接消除逐字节反射开销 |
| [v1.0.0-rc.48](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.48) | [70e6af64 / #8148](https://github.com/QuantumNous/new-api/commit/70e6af6429824f99de98e96e65254c8c2ef7ee0b)：禁用字段过滤使用 ojson.Doc；多次参数覆盖共用文档，最后统一生成字节 | 过滤不再将整个正文解析到 map 后重新编码；覆盖操作也避免每项都复制整包。达到本补丁第二项的目标，但实现不同、适用范围更广 |
| [v1.0.0-rc.50](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.50) | [f3d8b737 / #8311](https://github.com/QuantumNous/new-api/commit/f3d8b737b64842df0365c2a6696c276fbc409193)：内存正文允许只读借用解析，磁盘正文经独立 reader 按需读取；元数据提取不再为小字段完整载入磁盘正文 | 进一步减少入口副本；修复透传重试共用游标导致正文残缺的问题。只读借用之后仍要求对每次尝试的可变请求做克隆 |

截至核查时，最新发布版本为 [v1.0.0-rc.51](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.51)，提交 `c9c6da2786b6a0912215127e78e28fbc6650c85f`（2026-11-13 发布）。版本名称仍带 rc；GitHub 发布对象的 prerelease 标志为 false。这些优化在该版本中仍然存在。

实际核查源码：

- [common/copy.go](https://github.com/QuantumNous/new-api/blob/c9c6da2786b6a0912215127e78e28fbc6650c85f/common/copy.go)：RawMessage 转换器。
- [relay/common/relay_info.go](https://github.com/QuantumNous/new-api/blob/c9c6da2786b6a0912215127e78e28fbc6650c85f/relay/common/relay_info.go#L1212)：无修改快速返回，以及 ojson.Doc 定点删除。
- [relay/common/override.go](https://github.com/QuantumNous/new-api/blob/c9c6da2786b6a0912215127e78e28fbc6650c85f/relay/common/override.go#L859)：参数覆盖共用文档。
- [common/gin.go](https://github.com/QuantumNous/new-api/blob/c9c6da2786b6a0912215127e78e28fbc6650c85f/common/gin.go#L119) 与 [common/body_storage.go](https://github.com/QuantumNous/new-api/blob/c9c6da2786b6a0912215127e78e28fbc6650c85f/common/body_storage.go#L46)：只读借用、独立 reader 和按路径读取。
- [relay/responses_request.go](https://github.com/QuantumNous/new-api/blob/c9c6da2786b6a0912215127e78e28fbc6650c85f/relay/responses_request.go#L25) 与 [relay/common/outbound_body.go](https://github.com/QuantumNous/new-api/blob/c9c6da2786b6a0912215127e78e28fbc6650c85f/relay/common/outbound_body.go)：确认 Responses 实际调用了改进后的复制、过滤和出站缓存。

上游仍然会克隆请求、序列化出站正文；大请求并发照样占用内存。某些参数操作和插件也可能额外读取/变换正文，不能把这些优化理解为消除了所有副本或 OOM 风险。

因此，留在 v0.13.2 时，本分支提供局部修复；若计划升级，则值得对 rc.51 做业务兼容性及完整链路验证，无需重复移植本分支的两项实现。升级后的实际内存上限仍需测量。本次没有构建发布镜像、部署或修改生产服务。
