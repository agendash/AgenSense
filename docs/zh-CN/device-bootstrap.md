# 设备自注册与远程配置

## 目标

设备应该能够主动找到自己的实例，而不是每次手工把所有 URL 和 key 敲进 WebUI。

这个机制要解决四件事：

- 设备第一次上线如何认领
- 设备如何知道自己该连接哪个 gateway
- 设备如何拉取或接收配置
- 设备如何在实例迁移时自动 rebind

## 推荐流程

### 1. 出厂身份

每个设备至少带一份低权限 bootstrap 身份：

- `device_id`
- `chip_id`
- `hardware_sku`
- `factory_claim_token`

`factory_claim_token` 只用于首次认领或恢复绑定，不用于长期会话。

### 2. 启动 bootstrap

设备第一次启动时调用：

`POST /v1/bootstrap`

请求体建议包含：

```json
{
  "device_id": "vdk-coreS3-001",
  "chip_id": "esp32s3-abcdef",
  "hardware_sku": "m5cores3-facekit-audio",
  "firmware_version": "1.2.0",
  "firmware_channel": "stable",
  "capabilities": {
    "usb_hid": true,
    "usb_mic": true,
    "display": "lcd",
    "touch": true,
    "encoder": true,
    "cellular": false
  },
  "claim_token": "factory-claim-token"
}
```

### 3. bootstrap 响应

服务端返回：

```json
{
  "tenant_id": "home-lab",
  "instance_id": "cn-shanghai-main",
  "device_token": "jwt-or-pat",
  "ws_url": "wss://gw.example.com/v1/session/ws",
  "config_version": 12,
  "config": {
    "voice": {
      "enabled": true
    },
    "providers": {
      "profile": "default-cn"
    }
  },
  "retry_hint_sec": 30
}
```

### 4. 常规会话

设备之后通过 `device_token` 连接实时网关，不再使用 claim token。

### 5. 配置更新

配置更新有两条路径：

- 设备启动时主动拉取
- 在线时由服务端通过 WebSocket push

### 6. 迁移与 rebind

如果实例迁移，控制面可以把设备的 `instance_id` 指向新集群。设备下次重连时拿到新的 `ws_url`。

不要要求用户手工重刷 URL。

## 配置版本模型

每个设备维护：

- `desired_config_version`
- `reported_config_version`

流程：

1. 控制面更新配置，写入 `desired_config_version`
2. 实时网关推 `config.snapshot` 或 `config.patch`
3. 设备应用成功后回 `config.ack`
4. 服务端更新 `reported_config_version`

这样你就知道哪些设备配置已生效，哪些设备还没跟上。

## 设备主动“配置自己的实例”

如果你的意思是允许设备在现场自行绑定到一个用户拥有的实例，建议这样做：

### 模式 A：二维码 / 配对码认领

- 用户先在后台创建一个待认领实例或安装位点
- 生成一次性配对码或二维码
- 设备输入 / 扫描 / 通过临时 AP 页提交配对码
- 服务端据此把设备挂到指定 instance

### 模式 B：本地 AP + 远程 bootstrap

- 设备首次启动开 AP
- 用户只填最小信息：配对码 / bootstrap URL
- 设备自行向控制面认领

### 模式 C：预置 tenant installer token

- 用于批量安装
- 安装工人只需让设备联网
- 设备凭 installer token 自动挂到某个 tenant 的默认实例

## 安全边界

### claim token 只能做低权限操作

它不能：

- 直接下发 HID 动作
- 直接访问语音会话
- 长期复用

### 正式会话必须用短期 token

推荐：

- bootstrap 后签发短期 `device_token`
- 长期刷新用 `refresh_token` 或重新 bootstrap

### 配置下发必须带版本与签名

至少要满足：

- 有版本号
- 有来源认证
- 有幂等 ACK

## 失败场景

### bootstrap 服务不可达

设备回退到：

- 上次成功的 `ws_url`
- 上次成功配置快照
- 指数退避重试

### 设备配置损坏

设备可以触发：

- 回滚到最近一次成功版本
- 清除覆盖层，只保留实例模板

### 实例迁移中断

设备应支持多个 bootstrap 源：

- 主 bootstrap URL
- 备用 bootstrap URL
- 本地缓存旧实例
