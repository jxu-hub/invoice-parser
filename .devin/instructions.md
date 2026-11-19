# invoice-parser 作业约定

给在这里动手的 agent / 协作者。目标是让 200 张 golden 表的一次通过率
**只升不降**，具体数字见 `RESULTS.md`。

## 铁律

1. **不许改 `testdata/`**。fixture 和 golden 是基线，不是产出。
   要为新毛病加表，用 `cmd/genfixtures` 增加类别并在 `labels.csv` 里标好，
   然后在 PR 描述里写清"新增了几张表、v1 在新表上过几张"。
2. **不许改 `internal/parser/legacy.go` 的行为**。它是历史基线，
   `legacy_test.go` 把它的 137/200 和每类失败数写死了。
   想让这个数字变大，只能让 v2 变大。
3. **每改一版必须带 before/after**。格式固定：

   ```
   baseline: 200/200 (unchanged)
   baseline: 137/200 -> 195/200
   ```

   只有 after 没有 before 的 PR 直接打回。
4. **一次只解决一类失败**。第 2 个 PR 之后，一个 PR 里出现两个类别混在一起，
   回滚时没人分得清哪一半坏了。
5. `go test ./...` 必须全绿，`go vet ./...` 必须干净，`gofmt -l .` 必须为空。

## 做事顺序

1. 先跑 `go run ./scripts/baseline.go` 复现当前数字。
   **数对不上就停下来说明**，不要在数字对不上的情况下继续改。
2. 用 `go run ./cmd/inspect -file <表>` 看这一类的表到底错在哪一层：
   表头行、列类型、还是单值解析。
3. 只改对应的那一层，跑 `go test ./...`，再跑 baseline 对比。
4. PR 标题写清层次：`parser: header detection via kind consistency`、
   `parser: money normalization`、`collector: structured logging`。
   PR 描述里贴 before/after 数字。**不要自己 merge。**

## 代码口味

- 不引入第三方依赖。xlsx 读写都是手写的最小实现，这是刻意的：
  少一个依赖就少一个线上排障点。
- 注释写"为什么"，不写"是什么"。凡是反直觉的规则（比如日期必须 1900–2100、
  单号必须有前导零），注释里要写清楚它挡住了什么具体的坏数据。
- 兜底逻辑只在数量关系明确时才猜。**猜错一列比缺一列代价大。**
- 隔离行不是失败：让一行进隔离区是正常结果，让整表失败不是。

## 不要做的事

- 不要为了让测试变绿去改 golden。
- 不要加"自动修复 golden"的脚本。
- 不要在解析主路径里调大模型。模型只处理隔离行，且必须过 schema 校验。
- 不要用 `recover()` 把 panic 吞掉当成错误处理。
