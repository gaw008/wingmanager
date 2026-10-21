# 个人房东开源 PMS：项目规划

> 项目名 **wingmanager**。
> 这份文档是项目的唯一规划来源；代码与它冲突时，先改文档再改代码。

---

## 1. 定位

**给自己管房的个人房东用**，1–20 个单元，同时有短租（Airbnb / VRBO）和长租。
房东自己部署：家里电脑、NAS 或一台 5 美元的 VPS。下载一个文件就能跑。

一句话目标：**房东每天打开它，能回答三个问题——今天谁入住/退房、谁欠我钱、今年赚了多少。**

### 非目标（永远不做，除非改这份文档）

| 不做 | 原因 |
|---|---|
| 多租户 / SaaS | 一个房东一个实例，彻底消掉租户解析、隔离、计费 |
| 渠道管理器（Channex 类）、双向同步 OTA | 要付费 API、要处理推送失败和对账；个人房东用 iCal 就够 |
| 动态定价 | 独立的大问题，PriceLabs 等已经做得很好 |
| CRM、线索管理 | 个人房东没有销售漏斗 |
| AI 助理、客服机器人 | 核心稳定前不加 |
| 业主门户（Owner Portal） | 那是物业公司管别人房子的需求，不是房东管自己房子 |
| 保洁 App、GPS 追踪 | 超出个人房东规模 |
| 在线收租（v1） | 涉及外部资金流和 webhook，放到 v1 之后单独设计 |

---

## 2. 词汇表（代码、界面、文档统一用这些词）

| 词 | 含义 | 不要混用成 |
|---|---|---|
| **Property** 房产 | 一个地址 | — |
| **Unit** 单元 | 可以独立出租的部分。每个房产至少一个单元；整租房产 = 一个单元 | 房间/房源混叫 |
| **Booking** 短租订单 | 按晚计的入住，或日历屏蔽（`kind = block`） | Reservation、Event |
| **Lease** 租约 | 按月计的长租或中租 | Contract |
| **Guest** 客人 | Booking 里的人 | — |
| **Tenant** 租客 | Lease 里的人。**这个词只表示长租租客，永远不表示别的** | 系统租户、客户 |
| **Person** 人 | Guest 和 Tenant 都存在 `people` 表，身份由关系决定 | — |
| **Ledger entry** 账目 | 一笔钱的记录，见 §5 | Transaction、Payment 混用 |
| **Charge** 应收 | 租客欠的钱（房租、滞纳金…） | Invoice、Bill |
| **Payment** 收款 | 租客付的钱 | — |
| **Feed** 日历源 | 一个外部 iCal 链接 | Channel、Integration |

**日期规则（全系统唯一）**
- 入住和租约只用**日期**（`YYYY-MM-DD`），不用时间戳。"今天"按 `settings.timezone` 算。
- `bookings.check_out` 是**退房日**，不含这一晚。同一天一个退房一个入住**不算冲突**。
- `leases.ends_on` 是租约**最后一天**（含），符合租约文件的写法。
- 代码里比较区间时，统一转成半开区间 `[from, to)`，转换只在 `occupancy` 包的一个函数里做。

---

## 3. 技术选型

| 选择 | 理由 |
|---|---|
| **Go**（最新稳定版） | 语言小、写法单一、编译期检查；编译成单个可执行文件，跨平台 |
| **SQLite**，驱动 `modernc.org/sqlite`（纯 Go，无 CGO） | 数据就是一个文件，备份=拷文件；单写入者让并发问题天然简单 |
| **sqlc** | SQL 是唯一事实来源，Go 类型从 SQL 生成，不手写 ORM 映射 |
| 标准库 `net/http` + `html/template` | 不引入 Web 框架 |
| **htmx**（文件放进仓库，不走 CDN） | 局部刷新，不需要前端构建、不需要 API 层 |
| 自有设计系统（`internal/web/static/app.css`） | 原型确定的设计变量与组件，零前端构建 |
| 迁移：编号 `.sql` 文件嵌入二进制 + 自写约 50 行迁移器 | 少一个依赖；每个迁移在一个事务里只执行一次 |
| 运行参数：命令行参数 `--addr`、`--data-dir`（默认值写在 `--help` 里） | 不用环境变量；房东不需要写配置文件就能跑。业务设置（时区、币种、语言）存在 `settings` 表 |

**依赖白名单**（新增依赖要改这份文档）：
`modernc.org/sqlite`、`golang.org/x/crypto`（bcrypt）、`github.com/emersion/go-ical`（iCal 解析，处理折行和时区）、sqlc（仅开发时）。

---

## 4. 数据模型（13 张表）

约束尽量写进数据库，让错误数据进不去。

```sql
-- 账号与设置 -------------------------------------------------------
CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  email         TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);  -- v1 只有一个用户（房东本人），见 §12

CREATE TABLE sessions (
  token_hash BLOB PRIMARY KEY,           -- 只存哈希，不存明文 token
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL
);

CREATE TABLE settings (                  -- 恰好一行
  id            INTEGER PRIMARY KEY CHECK (id = 1),
  landlord_name TEXT NOT NULL DEFAULT '',
  timezone      TEXT NOT NULL DEFAULT 'America/Los_Angeles',
  currency      TEXT NOT NULL DEFAULT 'USD',
  locale        TEXT NOT NULL DEFAULT 'zh-CN' CHECK (locale IN ('zh-CN','en'))
);

-- 房产 ------------------------------------------------------------
CREATE TABLE properties (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL,
  address     TEXT NOT NULL DEFAULT '',
  notes       TEXT NOT NULL DEFAULT '',
  archived_at TEXT
);

CREATE TABLE units (
  id                INTEGER PRIMARY KEY,
  property_id       INTEGER NOT NULL REFERENCES properties(id),
  name              TEXT NOT NULL,
  ical_export_token TEXT NOT NULL UNIQUE,   -- 导出日历的秘密链接，可重置
  archived_at       TEXT,
  UNIQUE (property_id, name)
);

CREATE TABLE people (
  id    INTEGER PRIMARY KEY,
  name  TEXT NOT NULL,
  email TEXT NOT NULL DEFAULT '',
  phone TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT ''
);

-- 短租 ------------------------------------------------------------
CREATE TABLE calendar_feeds (
  id              INTEGER PRIMARY KEY,
  unit_id         INTEGER NOT NULL REFERENCES units(id),
  channel         TEXT NOT NULL CHECK (channel IN ('airbnb','vrbo','booking','other')),
  url             TEXT NOT NULL,
  sync_generation INTEGER NOT NULL DEFAULT 0,  -- 每次成功同步 +1，防止旧快照覆盖新快照
  last_attempt_at TEXT,
  last_success_at TEXT,
  last_error      TEXT,
  UNIQUE (unit_id, url)
);

CREATE TABLE bookings (
  id           INTEGER PRIMARY KEY,
  unit_id      INTEGER NOT NULL REFERENCES units(id),
  kind         TEXT NOT NULL CHECK (kind IN ('stay','block')),
  status       TEXT NOT NULL CHECK (status IN ('confirmed','cancelled')),
  check_in     TEXT NOT NULL,              -- 入住日
  check_out    TEXT NOT NULL,              -- 退房日（不含这一晚）
  channel      TEXT NOT NULL CHECK (channel IN ('direct','airbnb','vrbo','booking','other')),
  guest_id     INTEGER REFERENCES people(id),
  feed_id      INTEGER REFERENCES calendar_feeds(id),
  external_uid TEXT,
  notes        TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
  CHECK (check_out > check_in),
  CHECK ((feed_id IS NULL) = (external_uid IS NULL)),  -- 导入的必有来源和 UID，手动的都没有
  UNIQUE (feed_id, external_uid)
);
-- 来源不单独存字段：feed_id 为空 = 手动创建，非空 = iCal 导入。

-- 长租 ------------------------------------------------------------
CREATE TABLE leases (
  id            INTEGER PRIMARY KEY,
  unit_id       INTEGER NOT NULL REFERENCES units(id),
  status        TEXT NOT NULL CHECK (status IN ('draft','active','ended')),
  starts_on     TEXT NOT NULL,
  ends_on       TEXT,                      -- 最后一天（含）；NULL = 按月续租
  rent_cents    INTEGER NOT NULL CHECK (rent_cents > 0),
  due_day       INTEGER NOT NULL CHECK (due_day BETWEEN 1 AND 28),
  deposit_cents INTEGER NOT NULL DEFAULT 0 CHECK (deposit_cents >= 0),
  notes         TEXT NOT NULL DEFAULT '',
  CHECK (ends_on IS NULL OR ends_on >= starts_on)
);

CREATE TABLE lease_tenants (
  lease_id  INTEGER NOT NULL REFERENCES leases(id),
  person_id INTEGER NOT NULL REFERENCES people(id),
  PRIMARY KEY (lease_id, person_id)
);

-- 钱：全系统唯一的账本 ---------------------------------------------
CREATE TABLE ledger_entries (
  id           INTEGER PRIMARY KEY,
  property_id  INTEGER NOT NULL REFERENCES properties(id),
  lease_id     INTEGER REFERENCES leases(id),
  booking_id   INTEGER REFERENCES bookings(id),
  kind         TEXT NOT NULL CHECK (kind IN
                 ('charge','credit','payment','income','expense','deposit_in','deposit_out')),
  category     TEXT NOT NULL,              -- 合法值只在 ledger/categories.go 定义
  amount_cents INTEGER NOT NULL CHECK (amount_cents > 0),  -- 永远为正，含义由 kind 决定
  occurred_on  TEXT NOT NULL,
  period       TEXT,                       -- 'YYYY-MM'，只用于自动生成的房租应收
  memo         TEXT NOT NULL DEFAULT '',
  voided_at    TEXT,                       -- 账目不改不删：作废 + 新记一笔
  void_reason  TEXT,
  created_at   TEXT NOT NULL DEFAULT (datetime('now')),
  CHECK (kind NOT IN ('charge','credit','payment','deposit_in','deposit_out') OR lease_id IS NOT NULL),
  CHECK (period IS NULL OR (kind = 'charge' AND category = 'rent'))
);
-- 每份租约每个月最多一笔自动房租应收。这条索引就是"房租生成可以重复跑"的保证。
CREATE UNIQUE INDEX one_rent_charge_per_period
  ON ledger_entries (lease_id, period) WHERE period IS NOT NULL;

-- 任务与文件 ------------------------------------------------------
CREATE TABLE tasks (
  id          INTEGER PRIMARY KEY,
  property_id INTEGER NOT NULL REFERENCES properties(id),
  unit_id     INTEGER REFERENCES units(id),
  booking_id  INTEGER REFERENCES bookings(id),
  kind        TEXT NOT NULL CHECK (kind IN ('cleaning','maintenance','other')),
  title       TEXT NOT NULL,
  due_on      TEXT,
  status      TEXT NOT NULL CHECK (status IN ('open','done','cancelled')),
  assignee    TEXT NOT NULL DEFAULT '',    -- 自由文本，v1 不做账号
  notes       TEXT NOT NULL DEFAULT '',
  done_at     TEXT
);
-- 每个短租订单最多一个保洁任务。
CREATE UNIQUE INDEX one_cleaning_per_booking
  ON tasks (booking_id) WHERE kind = 'cleaning';

CREATE TABLE documents (
  id          INTEGER PRIMARY KEY,
  property_id INTEGER REFERENCES properties(id),
  unit_id     INTEGER REFERENCES units(id),
  lease_id    INTEGER REFERENCES leases(id),
  person_id   INTEGER REFERENCES people(id),
  filename    TEXT NOT NULL,              -- 原始文件名；文件本体存在 <data-dir>/files/<id>，不做去重
  mime        TEXT NOT NULL,
  size_bytes  INTEGER NOT NULL,
  uploaded_at TEXT NOT NULL DEFAULT (datetime('now'))
);
```

### 字段归属（谁能改什么）

| 数据 | 唯一权威 | 规则 |
|---|---|---|
| 导入订单的日期、状态 | 外部 iCal | 本地界面只读；下次同步会覆盖 |
| 导入订单的客人、备注、收入 | 房东 | 同步永远不碰这些字段 |
| 手动订单 | 房东 | 全部可改 |
| 日历 | 无（不建表） | 由 bookings + leases 查询得出 |
| 租客欠款 | 账本 | 余额实时计算，不存 |
| 未来房租金额 | `leases.rent_cents` | 改它只影响**之后**生成的应收，已生成的不变 |

---

## 5. 账本规则

一张表、七种 kind，含义固定：

| kind | 含义 | 影响 |
|---|---|---|
| `charge` | 租客应付（房租、滞纳金、水电、赔偿） | 租客余额 + |
| `credit` | 给租客减免（让租、折扣） | 租客余额 − |
| `payment` | 租客付款 | 租客余额 −，现金收入 + |
| `income` | 其他收入（短租平台打款等） | 现金收入 + |
| `expense` | 支出（维修、保险、税…） | 现金支出 + |
| `deposit_in` / `deposit_out` | 收押金 / 退押金 | 只影响押金余额，**不算收入** |

- **租客余额** = Σcharge − Σcredit − Σpayment（不含已作废）。v1 不做"某笔付款对应某笔应收"的分配，界面按先进先出展示。
- **报表按现金制**：收入 = payment + income，支出 = expense。
- **自动生成的房租应收不能作废**，改错用 `credit` 或补一笔 `charge`。原因：作废后定时任务会以为这个月没生成，又生成一次。
- **类别**只定义在 `ledger/categories.go` 一处，支出类别对应美国 Schedule E 各行，方便报税导出。
- **首月按天折算**：租约不从 `due_day` 开始时，首月房租 = 月租 × 实际天数 / 当月天数，四舍五入到分。规则只在 `lease.FirstPeriodRent()` 一处。

---

## 6. 核心流程：五种情况下的数据状态

### 6.1 iCal 导入同步（每个 feed 每 15 分钟一次）

步骤：拉取 → 解析 → 在一个 `BEGIN IMMEDIATE` 事务里：按 `(feed_id, external_uid)` upsert；feed 里消失的**未来**订单标记为 cancelled；新增或恢复的 stay 建保洁任务，取消的 stay 把保洁任务标为 cancelled；`sync_generation + 1`。

| 情况 | 结果 |
|---|---|
| 正常 | 本地 = 外部快照 |
| 重复执行 | upsert，结果一样 |
| 并发（同一 feed 两次同步） | 进程内对每个 feed 单飞（single-flight）；SQLite 写锁再串行一次 |
| 乱序（旧快照晚到） | 开始时记下 `sync_generation`，提交前若已变就丢弃本次结果 |
| 做到一半失败 | 事务回滚，什么都没变；记录 `last_error` |

**必须防住的危险情况**
- 拉取失败、返回 HTML 错误页、返回 200 但内容为空 → **不能当成"所有订单都取消了"**。规则：非 2xx 或解析失败直接放弃；如果解析出 0 个事件、但本地还有未来订单，不取消，标为警告。
- Airbnb 的 feed 会自动删掉过去的事件 → 只对 `check_out >= 今天` 的订单执行"消失即取消"。
- Airbnb 里的 "Not available" 是屏蔽，"Reserved" 是订单 → 分别映射为 `block` 和 `stay`，规则写在 `ical/classify.go` 一处。

### 6.2 自动生成房租应收（每天一次，启动时补跑一次）

步骤：对每份 active 租约，从 `starts_on` 所在月到"今天 + 提前天数"所在月，逐月 `INSERT ... ON CONFLICT DO NOTHING`。

| 情况 | 结果 |
|---|---|
| 正常 | 每月一笔 |
| 重复执行 | 唯一索引挡住，不会重复 |
| 并发 | 同上 |
| 乱序 | 不适用：月份是算出来的，不是外部事件 |
| 做到一半失败 / 电脑关机几天 | 每份租约一个事务；下次运行从头遍历，自动补齐漏掉的月份 |

### 6.3 手动创建订单或租约

在 `BEGIN IMMEDIATE` 事务里检查同一单元有没有重叠的 confirmed 订单或 active 租约，没有才插入。
说明：一般"先查再插"不能防重复，但 SQLite 的 IMMEDIATE 事务会先拿到全库写锁，查询和插入之间不可能有别的写入，所以这里是安全的。这个前提写成注释放在代码里。

**导入订单之间允许重叠**（两个平台真的超订了，这是事实，要记录下来），界面上标红警告，不拒绝。

### 6.4 首次启动设置

`users` 表为空时显示设置页，创建房东账号。两个浏览器同时提交：`BEGIN IMMEDIATE` 里再查一次表是否为空，后提交的那个失败。

---

## 7. 功能清单（v0.1）

**短租**
- 手动订单 / 屏蔽的增删改；冲突提示
- 每个单元配置多个 iCal 导入源，显示同步状态和错误
- 每个单元一个 iCal 导出链接（给各平台互相屏蔽）
- 日历视图：按月、按单元，订单和租约画在一起
- 退房日自动生成保洁任务

**长租**
- 租约：草稿 → 生效 → 结束；多个租客；按月续租
- 自动生成房租应收；手动记收款、减免、其他应收
- 每个租客的余额和流水；逾期列表
- 押金收取和退还记录

**账本与报表**
- 支出、其他收入的记录（可附收据文件）
- 按房产、按年的收支表（现金制）
- 按 Schedule E 类别导出 CSV

**通用**
- 首页"今天"：今天入住/退房、逾期租客、待办任务、同步出错的日历源
- 任务：保洁、维修、其他
- 文件：挂在房产、租约或人上
- 中文 / 英文界面
- 命令行：`serve`、`backup <file>`、`reset-password`、`seed-demo`

---

## 8. 代码结构

```
cmd/wingmanager/main.go    入口和命令行子命令
internal/
  db/                      连接、迁移器、migrations/*.sql
  occupancy/               区间换算和重叠判断（唯一一处）
  property/                房产、单元
  person/
  booking/                 短租订单：唯一能写 bookings 表的包
  ical/                    解析、分类、同步、导出
  lease/                   租约、首月折算
  ledger/                  账本、类别、余额、报表
  task/
  document/
  jobs/                    进程内定时器：iCal 同步、房租生成
  web/                     handler、模板、静态文件、i18n 文案
docs/
  PLAN.md                  本文件
  decisions/NNNN-*.md      架构决策记录
AGENTS.md                  给 AI 的简短说明：只写不变量和命令，不写会过期的结构描述
README.md
```

**规则**
- 每个领域包的 SQL 查询放在自己目录里，sqlc 分包生成。**只有 `booking` 包里有写 `bookings` 的查询**，别的包想写也写不了，编译器负责保证"唯一写入口"。
- handler 只调用领域包的函数，不写 SQL。
- 状态值、类别、渠道这些枚举，每种只在一个 Go 文件里定义，和数据库的 CHECK 约束一一对应（有测试比对两边）。

---

## 9. 测试策略

- 所有测试用**真实 SQLite**（每个测试一个临时文件，跑完迁移），不 mock 数据库。
- 只 mock 网络：iCal 拉取用 `httptest` 本地服务器，返回仓库里的样例文件（Airbnb、VRBO、Booking.com 的真实格式，去掉个人信息）。
- CI：`go vet` → `staticcheck` → `go test -race ./...` → 构建各平台二进制。

**最危险的 3 个失败场景，以及覆盖它们的测试**

| 场景 | 后果 | 测试 |
|---|---|---|
| iCal 返回空内容或错误页 | 所有未来订单被误取消，房子被重复出租 | `ical`：空 feed、HTML 错误页、500、超时，四种都不能取消订单 |
| 房租生成重复执行或漏跑 | 租客被重复收租，或少收 | `ledger`：连续跑两次；跳过 5 天再跑；并发跑两个 |
| 同日交接被判冲突，或真冲突没发现 | 拒绝合法订单，或超订 | `occupancy`：边界表驱动测试（同日交接、包含、相邻、租约含当天结束） |

---

## 10. 里程碑

一次只做一个，上一个的完成标志达成后才开始下一个。时间按每天专注开发、AI 辅助估算。

| # | 内容 | 耗时 | 完成标志 |
|---|---|---|---|
| M0 | 决定 §12 的待定项 | 0.5 天 | ✅ 2026-10-20 完成，见 `docs/decisions/0001-m0.md` |
| M1 | 骨架：Go 模块、迁移器、首次设置和登录、页面布局、i18n、CI | 1.5 天 | ✅ 2026-10-20 |
| M2 | 房产、单元、人 | 1 天 | ✅ 2026-10-20 |
| M3 | 短租：`occupancy`、手动订单、日历视图 | 2 天 | ✅ 2026-10-20 |
| M4 | iCal 导入导出、同步状态、保洁任务 | 3 天 | ✅ 2026-10-20（真实 Airbnb 链接未验证） |
| M5 | 长租：租约、租客、房租生成、收款、余额、押金 | 3 天 | ✅ 2026-10-20 |
| M6 | 账本：支出、收入、收支表、Schedule E 导出 | 2 天 | ✅ 2026-10-20 |
| M7 | 首页"今天"、任务、文件 | 2 天 | ✅ 2026-10-20 |
| M8 | 发布 v0.1：README、截图、演示数据、备份恢复、Docker、各平台二进制 | 2 天 | ✅ 2026-10-20（Docker 构建、发布工作流未实际运行） |
| M9 | 界面与体验：设计系统、信息结构、全站改版（决策 0011，原型见 `docs/prototype/`） | 3 天 | ✅ 2026-10-21（发版验收清单 `docs/ACCEPTANCE.md`） |

**合计约 17 个工作日。**

### v0.1 之后（先不承诺）

- v0.2：Airbnb / VRBO 打款 CSV 导入（自动生成短租收入）；SMTP 邮件提醒（房租到期、明天退房）；从电子表格导入租约
- v0.3：租客只读链接（查看余额和流水）；租客提交维修请求
- 以后：在线收租（Stripe）。需要单独写一份设计：幂等键、webhook 重复和乱序、"Stripe 已扣款但本地没记上"的对账

---

## 11. 从以往系统得到的教训

这些常见问题变成了本项目的规则和测试：

| 常见问题 | 本项目的做法 |
|---|---|
| 区间语义没在一处定义（结束日含不含当天，各处理解不同） | §2 日期规则 + `occupancy` 唯一一处 |
| iCal 里消失就取消，不在事务里，没有空 feed 保护 | §6.1 |
| 同一个订单存在两张表，多处写入 | 一张 `bookings`，日历不建表 |
| 应收再镜像一份到财务表 | 账本是唯一的钱 |
| 同一状态多种拼写（cancelled / canceled） | 枚举一处定义 + CHECK 约束 |
| 权限模块复制粘贴 | v1 不做权限 |
| 幂等靠"先查再插" | 唯一索引 + `ON CONFLICT` |
| 文档描述的结构和代码不符 | 文档只写不变量和词汇，结构让代码自己说明 |
| 测试 mock 数据库 | 真实 SQLite |

## 12. 已定决策

见 [`docs/decisions/0001-m0.md`](decisions/0001-m0.md)。新决策按编号追加文件，不改旧文件；推翻旧决策时新文件里写明"取代 0001 第 N 条"。
