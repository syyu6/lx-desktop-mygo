# AGENT.md

这个文件是给 AI 代理与本仓库贡献者的开发规范。`AGENTS.md`（测试约定）、
`RELEASING.md`（发版流程）仍是它们各自主题的唯一出处，这里不重复，只做汇总与索引。

## 这是什么

用 Go + [MyGo](https://github.com/egoist/mygo) 写的桌面音乐客户端，原生界面，没有 WebView。
主方向是 lx-music-desktop 的 MyGo 实现：导入音源脚本，一个应用听遍多个平台；内置网易云音乐客户端功能。

- 入口：`main.go` → `internal/app.Run()`
- UI：全部包在 `internal/app`，按页面拆成 `ui_*.go`
- 播放：`internal/player`（解码、缓存、重采样、增益）
- 来源：`internal/source`（内置源 + 音源脚本的解析链）、`internal/netease`、`internal/qq`、`internal/kuwo` 等
- 账号：`internal/account`（每个来源一个账号，可同时在线；QQ 与微信扫码在 `internal/qq/auth*.go`）
- 网络：`internal/network` 统一给出所有 HTTP 客户端，走系统代理
- `ref/lx-music-desktop`、`ref/mygo-qyin` 是参考实现，只读，不要改

## 开发与验证

```bash
go build ./...                      # 编译
go test ./internal/...              # 提交前必跑
go tool mygo dev                    # 开发模式运行（Dev 版）
go test ./internal/... -run TestX -count=5   # 新写的 UI 测试跑 5 次，查抖动
scripts/reset.sh                    # 清掉设置/登录/会话，像首次打开；--keep-login、--songs
```

只改 UI 想快速看效果，用 `go build -o /tmp/lxmuc-dev .` 起一个二进制比 `mygo dev` 更快迭代；
但要验证打包与更新行为，仍须走 `go tool mygo dev` / `go tool mygo build`。

## 代码约定

- **注释写“为什么”，不写“做了什么”。** 仓库里成段的注释解释取舍与背景，例如
  `internal/app/backdrop.go` 顶部、`internal/player/player.go` 的 `normGain`。
  新增逻辑请沿用这个密度与语气，不要写复述代码的注释。
- **中文面向用户，英文面向代码。** UI 文案、CHANGELOG、README、提交信息用中文；
  标识符、注释、测试注释用英文。
- **错误信息给人看。** 播放失败要能区分“需要 VIP”“地址打不开”“其它来源也没有”，
  不要把底层 error 直接抛到界面上。
- **不引入第三方来源内容。** 音源脚本永远不写进仓库，`internal/lxsource/testdata/`
  只放自造的 fixture；真实脚本只存在于用户的 Application Support 下做本地验证。
- **密钥不进仓库。** 更新私钥在 `~/Library/Application Support/mygo/update-keys/`，
  `mygo.json` 里只放公钥。
- **借用别人的协议实现要留痕。** `internal/qq` 照 `qq-music-api`（MIT）写的，
  `THIRD_PARTY_NOTICES.md` 里保留它的版权与许可；再借别的，同样补一条。
- 改了行为就更新 `CHANGELOG.md` 的「未发布」；面向用户的能力变化同步改 `README.md`。

## 测试

唯一的规范在 **`AGENTS.md`**，务必先读。要点：只测会坏的行为；UI 测试用
`ui.NewTester` + `newTestApp`（`internal/app/helpers_test.go`），按 accessibility label 找元素；
网络与剪贴板通过包级变量替换并 `defer` 还原；`LIVE` 才跑联网测试；`NM_FRAMES` 存图而不做像素断言。
不要测默认设置、表单行序、MyGo 自身的动画，不要依赖真实时钟。

## 性能预算

`internal/app/perf_test.go` 用 `framesAt` 数“空闲时每秒请求多少帧”。新增动画、定时器或
自绘内容时，在对应的 frame-budget 测试里确认它没有把空闲视图变成持续重绘——这直接决定内存
（见下节）。要求：视图空闲时 `FramePending()` 应为 false；`Painter.After` / `Context.After`
的等待帧不计入，别用它们掩盖持续重绘。

## 调试手册

以下都是本仓库实际用过、能复现问题的手法。临时探针文件一律以 `debug_` 前缀命名，
**调试完必须删掉并回滚散落的开关**，提交前用 `git status --porcelain | grep '^??'` 检查。

### 1. 测内存（macOS）

Activity Monitor 显示的是 **physical footprint**，不是 `ps` 的 RSS。用系统自带的：

```bash
footprint -p $(pgrep -f lxmuc-dev) | head -20
vmmap -w <pid> > /tmp/vmmap.txt        # 逐区域，按 DIRTY 排序
```

看这几个类别：`Owned physical footprint (unmapped) (graphics)`、`IOSurface`、
`IOAccelerator (graphics)`、`Untagged`、`Malloc Small`。Go 堆用 `runtime.ReadMemStats`
（`HeapAlloc` / `Sys`）另外看——两者常常相差一个数量级，别混淆。

孤立变量的办法（这次定位 250MB 用的就是它们）：

- `MYGO_GPU=0`：走 CPU 渲染，区分 GPU 与 CPU 侧成本
- 把窗口 `Hide()`：窗口不出帧时的基线
- `win.SetSize(...)`：改窗口像素面积，看是否按面积分配
- 一次性把某种动画关掉（临时包级开关），二分到具体绘制

结论要先抓住量级：本次实测“窗口只要 2 秒内出过帧就占住约 250MB，帧率与面积几乎无关，
停止出帧后 2 秒回落到约 100MB”。**先分清是哪一类成本，再决定改什么。**

### 2. 定位“一直在重绘”

持续重绘 = 内存不回落 = 掉电。两种看法：

- 打点计数：临时在每处 `AnimationFrame` / `AnimationFrameRate` / `After` 前加计数器，
  每 2 秒打印一次各来源的每秒次数（本仓库曾用 `NM_FRAMES_SRC` 这么做过，已删）。
- 用 MyGo 自带：`MYGO_FRAME_STATS=1`（或设毫秒阈值）会记录超时帧。

把“哪一处在请求帧”定下来，比猜快得多。

### 3. 音频问题

- 先看真实数据，不要先改代码：缓存里的 `<key>-<level>.json` 记录了该曲的 `gain` / `peak`。
  `cat` 全部 sidecar 统计实际分布（这次发现 76 首全是 `gain:1,peak:0`）。
- 用 `ffmpeg -af volumedetect` 测缓存音轨的真实峰值：
  `max_volume: 0.0 dB` + 非零 `histogram_0db` 表示已经顶到满刻度，任何正增益都会削波。
- 增益链路：`chain.go` 决定 `Stream.Gain/Peak` → `player.normGain` → `applyGain`（`volume² × gain`）
  → mixer 末尾 `min(max(s,-1),1)` 硬削波。怀疑破音就从这条链顺着查。
- 联网接口调试：写一个 `LIVE=1 go test -run TestXLive` 的临时 live 测试查看真实返回值，
  用完删除；`internal/netease` 已有 `live_test.go` 的写法可参考。
- 断断续续（stutter）优先怀疑喂不满音频输出：本仓库用 `audio.LatencyPlayback`
  （预混 250ms），确认解码线程没被 UI 抢死。

### 4. UI 快速看效果

`NM_FRAMES=<dir>` 跑对应 `Test...Frames` 测试会把界面存成 PNG 到该目录（无目录则 skip）。
适合在没有交互的情况下核对视觉改动，不要对像素做断言，只用于人工看。

### 5. 真实数据与隔离

开发版数据在 `~/Library/Application Support/LxMuc Dev/`，缓存在
`~/Library/Caches/LxMuc Dev/songs/`。做破坏性实验前用 `scripts/reset.sh` 或改用独立目录
（`mygo.App.SetPath`），不要把用户的缓存/登录弄坏。

## 提交

- 提交信息用中文，说清“改了什么、为什么”，与既有历史风格一致（读 `git log`）。
- 提交前：`go build ./...`、`go test ./internal/...`、确认没有遗留 `debug_*` 探针与临时开关。
- 只有用户明确要求时才提交或推送。
- 发版流程见 `RELEASING.md`。
