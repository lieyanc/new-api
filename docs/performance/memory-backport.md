# v0.13.2 第二阶段：大请求存储与 JSON 处理优化

日期：2026-11-18。分支：`v0.13.2-memory-opt`。

应用仍基于 v0.13.2；在第一阶段批量 RawMessage 深拷贝、Responses 序列化前过滤的基础上，选择性移植上游的性能改进。本次没有升级应用到 v1.0，没有引入新版插件系统、计费逻辑、数据库迁移或前端功能，也没有部署生产服务。

## 本次实现

1. **出站 JSON 使用 BodyStorage。** Responses、Chat Completions、Claude、Gemini、Embedding、Rerank、Image JSON 及 Chat→Responses 路径均已接入。磁盘缓存开启、正文超过阈值且有可用缓存空间时，序列化后的出站正文写入临时文件；等待上游时可以回收对应的堆缓冲区。未开启缓存或写盘失败时，沿用内存存储回退。
2. **每次上传和重放独立读取。** BodyStorage 新增独立 reader，保留 Content-Length，配置 Request.GetBody。关闭 HTTP 请求的 reader 不会关闭共享存储；原存储由所属请求/尝试清理。修复共用 seek 位置，以及收到响应头就提前关闭仍在上传的正文的问题。
3. **磁盘 JSON 直接从 reader 解码。** UnmarshalBodyReusable 的磁盘 JSON 路径不再先调用 Bytes() 整包分配；提取 model 等少量字段时可跳过大的未使用字段。只接受一个 JSON 值，尾随第二个值或垃圾数据仍被拒绝。通用 Marshal/Unmarshal 与内存正文继续使用原标准库实现，未引入共享可变请求字段的零拷贝行为。
4. **参数覆盖减少复制。** 普通顶层参数覆盖、无条件且使用简单路径的 set/delete，在一个 JSON 文档上集中修改，最后生成一次正文。复杂条件、通配符、负索引、头部同步等继续使用原规则处理逻辑，但处理过程改为字节切片，取消入口/出口的整包字符串转换。空字符串和冒号开头的字面键保留旧语义并回退到 RawMessage 映射处理。
5. **通用禁用字段过滤定点修改。** 标准 Responses 保留第一阶段的 DTO 过滤；其他 JSON 过滤不再把整包解码成 map[string]interface{} 再序列化。保留未修改数据的字段顺序、数字精度和原始表示，删除禁用字段的重复键；没有改动时返回原字节。
6. **减少关闭调试日志时的额外分配。** Chat/Gemini/Embedding 日志使用现有 logger 的延迟格式化接口，不再提前把大正文转成字符串。
7. **等待响应头有界。** 新增 `RELAY_RESPONSE_HEADER_TIMEOUT`，默认 1800 秒，0 或负数表示关闭；超大数值会被截断到安全的 duration 范围。默认、HTTP(S) 代理和 SOCKS 代理客户端均应用。只限制收到响应头之前的等待，响应头之后的流式读取不受这个超时限制。

仅新增上游使用的 `github.com/MoeForge/moego v0.1.0-alpha.2`，用于 ojson 定点编辑及磁盘 JSON decoder。应用 Go 版本声明仍为 1.25.1，其余依赖版本未升级。

## 来源和适配范围

参考上游提交：

- [fddf54cc](https://github.com/QuantumNous/new-api/commit/fddf54ccc5cf1c97c7a48c657c3cc204b3d9f68f)：出站正文缓存、字节参数覆盖。
- [70e6af64 / #8148](https://github.com/QuantumNous/new-api/commit/70e6af6429824f99de98e96e65254c8c2ef7ee0b)：定点 JSON 编辑。
- [f3d8b737 / #8311](https://github.com/QuantumNous/new-api/commit/f3d8b737b64842df0365c2a6696c276fbc409193)：磁盘解码、独立重放游标、上传生命周期。
- [b518d003 / #6949](https://github.com/QuantumNous/new-api/commit/b518d0033b670f5518b8a2f1cf8ea0142a9d1b8d)：响应头等待超时。

这是针对 v0.13.2 的适配，不是把上述提交整体 cherry-pick。复杂操作仍可能逐步分配、解码其目标或整个正文；multipart、未知长度入站正文、token/图片分析等没有完成全面流式化。

参数覆盖的实现额外避免了两类隐藏复制：读取根 JSON 使用 NoCopy 视图；重复键计数在原始正文上统一执行，避免每次编辑后读取文档根节点触发整包展开。

## 本地对比：第一阶段补丁 → 本次补丁

每请求 71,674,579 字节（68.354 MiB），25 段合成图片 data URL；没有生产数据，也没有上游网络请求。两组均启用磁盘缓存，并进行三项顶层参数覆盖。基线模拟第一阶段补丁的完整读取/覆盖/出站持有方式，两组均已包含第一阶段快速深拷贝和 DTO 过滤。

环境：Go 1.26.8、Linux amd64、Xeon W-2235，GOMAXPROCS=2，GOGC=100，GOMEMLIMIT=off；每组独立进程单次运行，未做统计重复试验。

| 场景 | 第一阶段耗时 | 本次耗时 | 第一阶段峰值 RSS | 本次峰值 RSS | 等待阶段保留 Go 堆：第一阶段 → 本次 |
|---|---:|---:|---:|---:|---:|
| 单请求 | 1.693 秒 | 0.669 秒 | 430.5 MiB | 295.3 MiB | 136.7 → 68.4 MiB |
| 4 并发整批 | 3.356 秒 | 1.401 秒 | 1525.3 MiB | 1393.0 MiB | 546.9 → 273.5 MiB |

四并发保留 Go 堆约减少 50%，过程累计分配约减少 37%，本次单次测量的峰值 RSS 约减少 9%。不能把等待阶段保留堆的下降比例说成进程峰值下降比例。

“等待阶段”是四个请求全部准备好、仍保留原始 DTO 和出站正文时的检查点；为了隔离存活对象，测量前做两次 GC，报告相对测试启动前的 HeapAlloc 增量。随后将出站正文读到 io.Discard，关闭存储。耗时包含准备、GC、读取出站正文和清理，不是生产端到端延迟。

峰值 RSS 使用 `/usr/bin/time -v`。磁盘缓存页会占用内核页缓存，在容器中通常计入 cgroup 内存；RSS 不包含全部文件缓存。这里不模拟容器 2 GiB 上限、上游延迟、重试、响应缓冲、token/图片处理或 sub2api，因此不能据此保证四个这种大小的请求在线上安全。

原始输出见 [results](results/)。运行：

```sh
go test -c -o relay-memory-bench.test ./relay/common
GOMAXPROCS=2 GOGC=100 GOMEMLIMIT=off /usr/bin/time -v ./relay-memory-bench.test -test.run='^$' -test.bench='^BenchmarkLargeResponsesRelayMemory$/^backport$/^concurrency=4$' -test.benchtime=1x -test.count=1
```

切换 phase1/backport 和 concurrency=1/4。它与第一阶段报告的简化流水线范围不同，不宜横向直接比较两份报告的绝对值。

## 验证

- 现有参数覆盖相关测试：条件、通配符、负索引、复制移动、头部操作、审计等，通过。
- 字段过滤、Responses DTO 过滤、显式零/false、RawMessage 副本隔离，通过。
- 新增内存/磁盘独立读指针、并发交错读取、关闭后重放及 owner 清理，通过；存储/磁盘解码测试的 race 检查通过。
- 新增磁盘解码与标准库结果对比、重复 model 键、无效/尾随 JSON、跳过大字段，且禁止调用 Bytes() 的测试，通过。
- 新增 307 重定向的正文与 Content-Length 校验、提前收到响应头时正文未被关闭，通过。
- 新增响应头超时在各类客户端的配置、0/负数/溢出，以及响应头之后的慢流仍能读完，通过。
- 新增字面参数键、大整数/原始数字表示、重复键、无操作/null 回退，通过。
- 受影响包编译和 git diff --check 通过。没有再次运行未改变的旧基准或无关测试。

## 使用条件

出站落盘复用已有磁盘缓存配置，无需新增开关；必须开启缓存，并为入站、出站临时文件及磁盘 I/O 留出空间。磁盘失败/缓存不可用时会回退到内存，不构成硬性内存准入限制。

新增响应头超时配置示例：

```dotenv
RELAY_RESPONSE_HEADER_TIMEOUT=1800
```

建议保留入口请求体上限和并发限制。此分支仍有深拷贝、序列化及原始 DTO 存活成本；用户端历史图片减量仍有价值。应用当前只更新了 GitHub 源码，尚未构建发布镜像或修改生产服务。
