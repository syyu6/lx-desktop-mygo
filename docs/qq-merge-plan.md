# QQ 音乐并入：多来源方案

把 `ref/mygo-qyin`（Lilt，QQ 音乐客户端）的能力并入本项目，让 QQ 音乐成为与网易云
并列的内置源：浏览、播放、账号，都按来源分开，设置里逐项控制。

## 背景：qyin 是什么关系

qyin 从上游 `mygo-cloudmusic` 的 `0.1.19` 附近分出，随后**删掉网易云**，把
`internal/netease` 换成 `internal/qqmusic`。它的 `internal/app` 与我们是同一份骨架，
只是把 `netease.` 逐字换成了 `qqmusic.`。

所以不能整体合并，只有两样东西值得取：

- `internal/qqmusic`：QQ 协议实现，3487 行代码 + 2500 行测试。
- 它为了 QQ 长出来的页面，以及几个我们确实缺的通用件。

本项目已经有 `internal/qq`（约 500 行）：能搜歌、搜歌单、开歌单、放匿名 128k/320k、
取歌词。差距在**浏览**（推荐、排行榜、歌手、专辑）与**账号**（登录、我的歌单、喜欢、
每日推荐、已购）。

## 一、目标架构

### 1.1 来源能力：接口断言，沿用现有机制

`internal/source` 今天按接口断言说一个源会什么，`Cap` 说监听者要它做什么，两者分开。
这个设计原样保留，只加接口：

```go
// 一个源的首页料，各来源各自有
Featureder  Featured(limit int) ([]music.Playlist, error)
Charted     Charts() ([]music.Playlist, error)
Dailier     Daily() ([]music.Track, error)            // QQ 需登录
Recommender Recommended() ([]music.Playlist, error)   // QQ 需登录
FMmer       PersonalFM(mode string) ([]music.Track, error)

// 详情页
Cataloguer  Artist(id int64) (*music.ArtistProfile, error)
            ArtistSongs(id int64, newest bool, limit int) ([]music.Track, error)
            ArtistAlbums(id int64, limit, offset int) ([]music.AlbumProfile, bool, error)
            Album(id int64) (*music.AlbumProfile, error)
            User(id int64) (*music.User, error)

// 与账号绑定的东西
Librarian   Mine() (made, saved []music.Playlist, err error)
            LikedIDs() ([]int64, error)
            Like(id int64, on bool) error

// 评论
Commenter   HasComments() bool
            Thread(t music.Track) string
            Comments(thread string, offset, limit int) (music.Comments, error)
            LikeComment(thread string, id int64, on bool) error

// 只有 QQ 有的
Extras      PurchasedSongs(limit, offset int) ([]music.Track, bool, error)
            PurchasedAlbums(limit, offset int) ([]music.AlbumProfile, bool, error)
            Membership() (music.Membership, error)
```

`Registry.Has(id, cap)` 仍决定设置里那个开关有没有东西；页面用
`reg.Get(id).(source.Charted)` 决定自己出不出来 —— 与 `ui_songlists.go` 的
`squareSource()` 完全同一套路，那条路已经跑通了。

配套：`netease.SearchType`、`SearchResult`、`ArtistProfile`、`AlbumProfile`、`User`、
`Comment`、`Comments`、`Play` 从 `internal/netease` 提到 `internal/music`。
`Track` / `Playlist` / `Stream` 已经是 alias，不用动。

### 1.2 页面：一个入口，页头分段

侧栏不新增条目。`为你推荐` / `每日推荐` / `排行榜` 各留一条，页头出现
`[ 网易云 | QQ ]` 分段，只在「≥2 个开启的来源都有这个能力」时显示。
分段控件从 `ui_songlists.go` 的 `squareSources()` 抽成通用的 `sourceTabs()`。

| 页面 | 改动 |
|---|---|
| 为你推荐 | `loadHome()` 改为按来源取；QQ 未登录时不出「今日为你推荐」，只出「推荐歌单」与「官方榜单」 |
| 每日推荐 | 按来源；QQ 未登录时页内说「登录 QQ 音乐后每天有一份」 |
| 排行榜 | 按来源 |
| 私人 FM | 按来源；QQ 只有「推荐」一档，档位与「不喜欢」按能力隐藏 |
| 歌单（广场） | 已有来源切换；QQ 需补 `Tags()` / `SquarePlaylists()` |
| 歌手 / 专辑 / 用户 | `route.src` 真正填上，`fetch*` 走注册表 |
| 搜索 | 单曲/歌单已合并多源；歌手/专辑改问各来源；用户、播客仍只有网易云 |

`neteaseOnly(page)` 改成 `noSourceProvides(page)`。`播客` 保持网易云独占。

### 1.3 账号：每个来源一个，同时在线

新包 `internal/account`：

```go
type Profile struct {
    Source   string // music.SourceNetease / music.SourceQQ
    UserID   int64
    Nickname string
    Avatar   string
    VIP      string // 来源说得出才有
}

type Account interface {
    Source() string
    Restore() bool                 // 从文件读回会话
    Profile() (*Profile, error)
    Logout()
}

type Login interface {             // 扫码那条路
    Source() string
    QRCode() (QR, error)           // QR: URL 与可选的 PNG，对话框统一收成一个
    Check(key string) (State, *Profile, error)
    Commit() error
}
```

- `app.profile` 保留为「当前主账号」的便捷取用，其下换成
  `a.accounts map[string]*accountState`，键是来源 id。
- `netease` 的会话是包级 cookie jar（`netease/netease.go` 的 `init`），
  所以「每个来源一个账号」刚好够用，**不需要重构 netease**。这是选这条路的理由。
- QQ 会话**不照抄 qyin 的 `mygo.Secrets`**（钥匙串），改存 `<userData>/qq-session`、
  `0600`，与 `netease/auth.go` 一致。0.1.26 刚踩过 ad-hoc 签名导致每次更新都弹钥匙串的坑。
- 登录对话框 `loginState` 加 `src`；QQ 多一个 QQ / 微信分段。微信路径用
  `refresh_token` 自动续期，QQ 路径不会（文案里要说清「过期需重扫」）。
- 登录错误按阶段中文化，移植 qyin 的 `LoginErrorMessage` 那套。

### 1.4 设置 → 来源 → 内置源

- 每个内置源一行，展开后是**能力 chips（已有）+ 账号行（头像 / 昵称 / 登录或退出）**。
- 顶部独立那条「网易云音乐账号」分组取消，并进各自来源行。
- `SourceOrder`、`LibrariesOff` / `SourcesOff` / `LyricsOff` / `CommentsOff` 不动。
- 音质先保持全局一个 `settings.Quality`。两源档位语义不同（QQ 按账号权限逐级回退），
  等两边都跑通再谈按来源。

### 1.5 侧栏页脚

- 都未登录 → 「登录」，菜单里列出来源选一个扫码。
- 已登录 ≥1 → 主账号头像 + 来源小徽标；菜单里每个账号一行，底部是 设置…。
- 「我的歌单 / 收藏的歌单」跨来源合并，行上加来源小字（`Pl.Src()` 已有），
  重名靠这行消歧。

### 1.6 一处隐患

`a.liked map[int64]bool` 用裸数字 id 当键，两个来源会撞号。改成
`map[string]bool`，键 `Track.Ident()`（现成的 `src/key` 形状），按来源分开加载。

## 二、分期

| # | 内容 | 体量 |
|---|---|---|
| 0 | 类型上提 + 能力接口 + `netease.Provider` 实现 + 抽 `sourceTabs` | M，约 30 文件机械改动 |
| 1 | 移植 `qqmusic` 的 catalog / charts / client / playback / lyrics / comments；补 `Tags` / `SquarePlaylists`；移植其测试 | L，新 `internal/qq` 约 2000 行 |
| 3 | 页面按来源：home / charts / songlists / artist / album / user / search | L |
| 2 | QQ + 微信登录全套；`internal/account`；对话框多来源；设置账号行；页脚多账号 | L，约 1000 行 |
| 4 | 已购音乐页 + 侧栏入口；会员徽章；收藏专辑 tab；简介 hover 卡片 | M |
| 5 | 缓存目录 + 迁移；系统代理；`THIRD_PARTY_NOTICES.md` | M |
| 6 | README / CHANGELOG / AGENTS.md；perf 预算；新 UI 测试 `-count=5` | S |

顺序 **0 → 1 → 3 → 2 → 4 → 5 → 6**。3 排在 2 前面，是为了让 1 的成果立刻可见、可测：
免登录那部分不碰账号，风险最低。

## 三、从 qyin 取什么、不取什么

### 取

- `internal/qqmusic` 的 catalog / charts / client / playback / lyrics / comments /
  recommendation / membership / purchased（后两者属阶段 4）。
- `auth.go` / `auth_wechat.go` / `auth_device.go` / `auth_errors.go` / `auth_response.go`。
- `ui_purchased.go`（已购音乐页）。
- `ui_description.go`（简介 hover 卡片）。
- `cache_directory.go` + `cache_migration.go`（缓存目录可选 + 重启迁移）。
- `internal/network/proxy.go`（系统代理）。
- `settings.CacheDir` / `PendingCacheDir` 两个字段与对应的迁移。

### 不取

- `website/`、`vendor/`、`bun.lock`、`package.json`、它自己的发版脚本。
- `patches/mygo-windows-menu-width.patch`：那是修 Windows **系统**菜单宽度，而本项目
  0.1.27 起菜单全部由窗口自绘（`PopoverMenu`），系统菜单一个不剩。
- `ui_quiet.go`、`pickup.go`、`session.go`：本项目已有同款。

### 明确不支持的 QQ 能力

`FMTrash`、`HeartMode`、`LikeComment`、`FollowArtist`、`RecentPlays`、播客与节目、
用户动态 —— qyin 里全是 `ErrUnsupported` 桩。页面上按能力隐藏，不显示空页。

## 四、风险与已知降级

1. **QQ 协议脆弱**：签名、设备指纹、风控都在。qyin 的 `*_live_test.go` 要一并移植并
   用 `LIVE=1` 实跑，否则只有 httptest 的假 responder 自证。
2. **QQ 歌单分类端点未实测**：qyin 完全没做广场。`Tags()` / `SquarePlaylists()` 要新写，
   候选是 `/soco/fcgi-bin/fcg_get_diss_by_tag.fcg`，但分类口径与返回形状必须先打一发
   真实请求确认。
3. **QQ 歌词没有逐字**：`yrc` 恒空，「律动」页对 QQ 歌曲退化成逐行滚动。要写进 CHANGELOG。
4. **QQ 评论分页依赖服务端游标**：换账号或重启后游标失效，要重新打开评论。保留这个
   行为并给出中文提示。
5. **许可证**：qyin 与本项目同为 AGPL-3.0，兼容；但 `qqmusic` 的协议实现基于
   `qq-music-api`（MIT），必须在本仓库补 `THIRD_PARTY_NOTICES.md` 并保留版权声明。

## 五、验收

- 阶段 0：`go test ./internal/...` 全绿，UI 无可见变化。
- 阶段 1：`LIVE=1` 实跑搜索、榜单、歌单、歌手、专辑、评论。
- 阶段 3：QQ 免登录即可浏览推荐、榜单、歌单广场、歌手、专辑；网易云的页面回归无变化。
- 阶段 2：两个账号同时在线，重启后各自恢复；退出其一不影响另一。
