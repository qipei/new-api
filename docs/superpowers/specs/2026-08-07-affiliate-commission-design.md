# 推广赚佣(邀请充值返佣)设计文档

日期:2026-08-07
分支:`feature/affiliate-commission`
参考:上游未合并 PR [QuantumNous/new-api#3495](https://github.com/QuantumNous/new-api/pull/3495)(同步埋点方案,仅参考思路)

## 1. 目标与范围

被邀请人(通过 `aff_code` 注册的用户)完成真实付费充值后,系统按管理员配置自动给邀请人发放佣金,并提供用户端佣金明细。

**做:**

- 核心返佣机制:固定额度 / 按比例两种模式
- 返佣次数模式:被邀请人前 N 笔付费订单返佣,或不限次数
- 佣金流水表 + 用户端明细页
- 管理后台配置界面

**不做(v1):**

- 退款/拒付后的佣金追回(流水里有 `topup_id` 可追溯,留待需要时做)
- 按用户/分组差异化佣金率
- 推广海报、二维码等营销工具(上游 Issue #3374 提到,后续再议)
- 上线前历史订单补发

## 2. 核心约束:最小化上游合并冲突

本仓库是 QuantumNous/new-api 的 fork,需要持续合并上游。因此采用**异步扫单**架构,不在任何支付回调中埋点:

- 不修改 `model/topup.go`、`controller/topup*.go` 等上游活跃文件
- 不给 `topups`、`users` 表加列
- 全部逻辑收敛在新文件中;对上游文件的触碰仅限:`main.go` 1 行(启动 worker)、`router/api-router.go` 约 2 行(新路由)、前端 section-registry 1 行(注册配置节)

## 3. 触发范围(已确认的产品决策)

- **仅真实付费订单**:`topups` 表中 `status = success` 的订单(易支付/Stripe/Creem/Waffo/订阅等)
- 兑换码、管理员直接赠送额度**不**返佣(它们不产生成功付费订单)
- 管理员补单(`ManualCompleteTopUp`)补的是真实支付订单,状态最终为 success,**正常计佣**
- 佣金入账到邀请人现有的 `aff_quota` / `aff_history` 字段,复用现有「划转到余额」提取流程

## 4. 配置项

新增独立配置文件(如 `setting/operation_setting/commission.go`,零冲突),序列化存 options 表:

| 配置项 | 类型 | 含义 |
|---|---|---|
| `commission_setting.enabled` | bool | 总开关,默认 false |
| `commission_setting.type` | string | `fixed`(固定额度)/ `percent`(按比例) |
| `commission_setting.value` | number | fixed:每笔固定额度(quota);percent:0–100 百分比 |
| `commission_setting.topup_count_limit` | int | 0 = 每笔付费订单都返佣;N>0 = 仅被邀请人前 N 笔成功付费订单返佣 |

校验规则(后端为准,前端同步校验):type 只能是 fixed/percent;percent 时 value ∈ (0, 100];fixed 时 value > 0;topup_count_limit ≥ 0。

### 佣金基数(percent 模式)

**基数 = 该订单的到账额度(credited quota),不是 `Money`。**

原因:各渠道 `Money` 语义/币种不统一(易支付为人民币实付,Stripe 为折算后美元),而到账额度跨渠道语义一致,对用户也最好解释(返佣 = 被邀请人到账额度 × 比例)。

到账额度换算镜像 `model/topup.go` 中 `ManualCompleteTopUp` 的既有规则:

- `payment_method = stripe`:`Money × QuotaPerUnit`
- `payment_method = creem`:`Amount`(已是额度)
- 其他(易支付/waffo 等):`Amount × QuotaPerUnit`

⚠️ 维护注意:此映射是对上游逻辑的镜像,合并上游时若发现 `ManualCompleteTopUp` 的换算分支有变化(如新增支付渠道),需同步更新。未知渠道走默认分支(`Amount × QuotaPerUnit`),即使不准确也被次数限制和比例上限约束,风险有界。

所有额度计算遵循项目计费安全不变量:使用 `common.QuotaFromFloatChecked` 等集中换算助手,禁止裸 `int()` 强转;佣金结果 ≤ 0 时跳过不发放。

## 5. 数据模型

新表 `commission_records`(新文件 `model/commission.go`,GORM AutoMigrate,兼容 SQLite/MySQL/PostgreSQL):

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | int, PK | |
| `topup_id` | int, **uniqueIndex** | 幂等兜底:同一订单只发一次 |
| `inviter_id` | int, index | 收佣人 |
| `invitee_id` | int, index | 充值人 |
| `topup_money` | float64 | 订单实付金额快照(审计用) |
| `credited_quota` | int | 订单到账额度快照(percent 基数) |
| `commission_quota` | int | 实际发放佣金额度 |
| `commission_type` | varchar | 发放时的 type 快照 |
| `commission_value` | float64 | 发放时的 value 快照 |
| `created_time` | int64 | |

不修改任何现有表。

## 6. 发放流程(异步 worker)

仅 master 节点(`common.IsMasterNode`)启动一个后台协程,默认每 30 秒扫一轮:

1. **游标扫描**:游标(上次扫描的完成时间水位)存 options 表。查询条件:
   `status = 'success' AND (complete_time >= cursor − overlap OR (complete_time = 0 AND create_time >= cursor − overlap))`,按 id 升序、limit 分批(如 200)。
   - 回看窗口 overlap = 10 分钟,解决订单乱序完成/时钟偏移导致的漏扫;重复扫到的订单靠 `topup_id` 唯一索引去重
   - `complete_time = 0` 回退用 `create_time`:已验证易支付回调只改状态不写 `complete_time`(`controller/topup.go` EpayNotify),其他渠道均写
2. **逐单判定**:
   - 功能未开启 → 本轮直接不扫
   - 充值用户 `inviter_id = 0`,或 `inviter_id = 自己`(脏数据防御)→ 跳过
   - 次数限制:`topup_count_limit = N > 0` 时,统计该充值用户全部时间内 `status = success` 订单中,本订单按 id 序是第几笔;序号 > N 则跳过(含上线前历史订单,即"注册后前 N 次"按全量算)
   - 计算佣金:fixed → value;percent → 到账额度 × value ÷ 100(经 `common.QuotaFromFloatChecked`)
3. **单笔事务发放**:插入 `commission_records`(唯一索引冲突 = 已发过,静默跳过)+ 邀请人 `aff_quota += x`、`aff_history += x`(原子 `gorm.Expr`)。事务成功后在事务外 `RecordLog` 给邀请人记日志(「邀请用户充值返佣 …」)
4. **游标推进**:整批处理完(含跳过)后,游标推进到本批最大完成时间;单笔发放失败记录 `SysError` 但不阻塞游标(下一轮回看窗口内会重试,超窗后需人工处理——日志可查)
5. **初始化**:首次启动时游标写为当前时间 → 历史订单不补发

## 7. API

- `GET /api/user/commission/records?page=&page_size=`(登录用户):当前用户作为邀请人的佣金流水,按时间倒序。返回字段:时间、被邀请人用户名(脱敏,如 `q***i`)、订单实付、佣金额度。新文件 `controller/commission.go`,路由挂在 `router/api-router.go` 用户组下
- 配置读写复用现有 options 机制(`/api/option/`),配置校验逻辑放在新配置文件的反序列化/校验函数中,不改 `controller/option.go` 的 switch(若现有 options 框架必须注册 key,则以最小行数注册)

## 8. 前端

- **钱包页**(`web/src/features/wallet/`):在现有邀请奖励卡片(`affiliate-rewards-card.tsx`)加「佣金明细」入口,弹窗分页表格展示流水。新组件文件 + 卡片内 1 处入口改动;wallet 是 fork 重写区域,冲突风险低
- **管理后台**(`web/src/features/system-settings/`):计费设置新增「推广返佣」配置节:开关、类型下拉、数值输入、次数限制输入(0 = 不限)。新组件文件 + `section-registry` 注册 1 行
- i18n:en/zh 及其余 locale 按项目规范补 key(英文原文为 key)

## 9. 防刷与安全

- 仅真实付费订单触发(最强防线)
- `topup_count_limit` 是运营侧防刷阀门(如设 3)
- 跳过自邀脏数据
- 佣金计算全链路走 `common/quota_math.go` 集中换算助手,不可能因溢出产生负佣金
- 幂等三层:游标 + 唯一索引 + 事务

## 10. 测试

- `model/commission_test.go`:发放事务幂等(同 topup_id 二次发放不重复)、percent/fixed 计算、次数限制判定、到账额度三分支换算(testify,表驱动)
- 扫描查询的 `complete_time = 0` 回退分支
- 配置校验边界(percent > 100、负值等)
- 手工验收:本地起服务,构造 success 订单,观察 worker 发放、明细页展示、划转提取

## 11. 实现前需二次确认的事项

- options 配置机制对新 key 的注册方式:确认能否完全不改 `controller/option.go`(以现有 `setting/operation_setting` 下其他配置文件的接入方式为准)
- 订阅流程(`model/subscription.go`)生成的 TopUp 订单的 `Money`/`Amount`/`payment_method` 语义,确认到账额度换算分支覆盖
