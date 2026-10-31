# 临时图片上传

## 管理员配置

系统设置 → 运维 → 文件存储。使用阿里云 OSS 私有 Bucket，标准存储，保持阻止公共访问开启、版本控制关闭。

- Endpoint 例如 `oss-cn-hangzhou.aliyuncs.com`；地域填写 `cn-hangzhou`，Bucket 只填名称。
- 自定义下载域名填写完整 HTTPS 域名，例如 `https://files.example.com`，需在 OSS 绑定并配置证书。留空使用 Bucket 公网域名；内网上传 Endpoint 会自动转换为公网下载 Endpoint。
- AK/SK 使用 RAM 子账号，只授权目标 Bucket 的上传目录，保存后不回传。留空保留原凭据；更换账号时请同时填写 ID 和 Secret。
- 默认目录 `uploads/images/`，签名 URL 默认 24 小时。URL 有效期与对象保留期是不同概念，文件被清理后链接即使未到期也不可用。
- 在 OSS 控制台手动设置该前缀对象 3 天后过期删除；系统只检查规则，不修改 Bucket 生命周期。不要设置阻止上游下载的防盗链规则。
- RAM 对象权限：`oss:PutObject`、`oss:GetObject`、`oss:DeleteObject`，Resource 为 `acs:oss:*:*:<bucket>/uploads/images/*`。
- 检查清理规则需要 Bucket 级 `oss:GetBucketLifecycle`，Resource 为 `acs:oss:*:*:<bucket>`。
- 先保存，再点击测试连接（写入、通过签名公网链接读取、删除测试文件），然后检查清理规则。关闭上传时仍可测试已保存的配置。

密钥随站点其他设置存储于数据库，数据库及备份应限制访问。连接测试会产生少量 OSS 请求、流量和临时对象，不调用付费模型。

## 上传接口

兼容 APIMart 的请求参数及成功响应字段：

```sh
curl https://your-site.example/v1/uploads/images \
  -H 'Authorization: Bearer YOUR_API_KEY' \
  -F 'file=@/path/to/photo.jpg'
```

唯一表单参数 `file`，`multipart/form-data`。支持 JPEG、PNG、GIF、WebP，默认最大 20 MiB，可由管理员调低。按实际图片头检测类型，不以扩展名或客户端声明为准。最大 1 亿像素。服务端不会重新编码图片。

```json
{
  "url": "https://files.example.com/uploads/images/2026/10/31/random.png?...",
  "filename": "photo.png",
  "content_type": "image/png",
  "bytes": 235680,
  "created_at": 1793455200
}
```

URL 允许持有者在有效期内下载，请将完整 URL（包括签名参数）传给模型的参考图字段。过期后重新上传。接口不接收 URL、Base64、视频或音频，不消耗模型额度，但 API Key 必须通过站点现有的状态、过期、额度、IP 和用户权限检查。

错误返回 `{"error":{"message":"...","type":"invalid_request_error"}}`；400 缺文件/无效图片，413 超过大小限制，429 上传限流（含 code:429），500 存储失败，503 未开启/配置或限流服务不可用。鉴权失败沿用现有 API Key 鉴权格式。

默认同一用户所有 Key 合计 10 次/分钟、1024 MiB/UTC 自然日。在读取请求体前预留请求次数（无效文件也计数），通过图片校验后预留实际字节，OSS 失败也计入限额；Redis 下多实例共享、原子检查，无 Redis 时为单实例内存限制（重启重置）。不会修改模型 RPM 或令牌已用额度。上传记录不保存签名 URL。

## 管理接口

仅超级管理员：GET/PUT `/api/option/object-storage`；POST `/api/option/object-storage/test`；POST `/api/option/object-storage/lifecycle`。通用 option 接口不允许读取或更新存储配置。

## 本地资源与清理

每个实例最大并发上传数默认 2，可配置 1–30。占满后返回 429，不排队，也不占模型接口并发名额。调低不终止现有上传，直到活跃数低于新阈值才接收新上传。

图片流式写入专用临时目录，单文件不超过配置大小（最高20 MiB）；正常中转文件占用约为最大并发数×最大文件大小。每次请求结束，无论成功失败，都关闭并删除临时文件。文件头检测不做图片解码、压缩或转码；上传 OSS 使用独立复用连接池，不使用模型的 HTTP 客户端连接池。请求体接收超时60秒、OSS上传超时90秒。上传路由不做全局请求体解压，也不进入模型用量统计或模型准入计数。

异常退出遗留的中转文件默认保留1小时，每30分钟清理一次；支持后台配置保留1–308小时、间隔5–1440分钟，以及手动清理。启动时也执行一次。清理分批扫描专用目录，仅移除匹配命名且超过保留时间的普通文件，跳过本实例的活跃文件、目录和软链接。多实例清理仅作用于各自本地目录，勿将该临时目录挂载为多进程共享目录。

本地清理不访问或删除 OSS 对象。OSS文件的3天生命周期需单独在OSS配置。已有中转残留占用与运行时缓冲不包含在正常并发×文件大小估算内。

## 配置编辑方式与必填项

文件存储提供可视化和 JSON 两种编辑方式，两者编辑同一份配置。JSON 使用接口字段名（如 `public_endpoint`），不包含只读的 `credentials_configured`。保存成功后清空界面中本次输入的凭据，不在浏览器本地持久化。JSON 语法或配置校验失败时不可提交。

启用上传时 Endpoint、地域和 Bucket 必填；首次启用须填写 AccessKey ID / Secret，已经保存凭据后可留空保留原值。自定义 HTTPS 域名为选填，空值使用 OSS Bucket 公网域名；前缀和各数值项必填但已预置默认值。若阿里云账户或所选地域要求绑定自定义域名，即使本系统字段为选填，也需按 OSS 要求完成绑定，并通过连接测试验证。

访问密钥获取：登录阿里云 RAM 控制台，进入「身份管理 → 用户 → 选择 RAM 用户 → 凭证管理 → 创建 AccessKey」。ID 和 Secret 成对填写，Secret 只在创建时显示。使用仅授权目标 Bucket 和上传目录的 RAM 用户，不使用主账号密钥。

官方指引：https://help.aliyun.com/zh/ram/user-guide/create-an-accesskey-pair
