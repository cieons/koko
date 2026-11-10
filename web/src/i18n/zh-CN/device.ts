import type en from "../en/device";

// The device area's messages: its pages and its API error codes.
export default {
  "device.title": "设备",
  "device.count.one": "{count} 台设备",
  "device.count.other": "{count} 台设备",
  "device.here": "当前设备",
  "device.revoked": "已吊销",
  "device.joined": "{date} 加入",
  "device.lastUsed": "上次使用：{ago}",
  "device.neverUsed": "尚未使用",
  "device.revokedOn": "{date} 吊销",
  "device.none": "{name} 没有设备。",
  "device.showRevoked": "显示已吊销的设备",
  "device.add": "添加设备",
  "device.add.titleFor": "为 {name} 添加设备",
  "device.add.lead": "一个加入码可供一台设备加入，仅可使用一次。",
  "device.add.name": "设备名称",
  "device.add.nameHint": "可留空。留空时，koko 依次命名为 device-1、device-2 等。",
  "device.add.submit": "签发加入码",
  "device.joinCode.title": "加入码已签发",
  "device.joinCode.label": "加入码",
  "device.joinCode.intro": "请在 {user} 的新设备 {device} 上运行以下命令。",
  "device.joinCode.expired": "此加入码已过期。请通过“添加设备”签发新的加入码。",
  "device.joinCode.forgotten": "此加入码已不再显示。请通过“添加设备”签发新的加入码。",
  "device.done": "完成",
  "device.revoke": "吊销",
  "device.revoke.label": "吊销 {name}",
  "device.revoke.title": "吊销 {name}",
  "device.revoke.consequence": "{name} 将立即断开且无法再次连接，由其签发的加入码随即失效。由其添加的设备不受影响。",
  "device.revoke.consequenceHere":
    "{name} 是此页面所经由的设备。吊销后，此页面将无法执行任何操作，此设备将删除其密钥，由其签发的加入码随即失效。由其添加的设备不受影响。",
  "device.revoke.consequenceLast":
    "{name} 是所有者的最后一台设备。吊销后，只能在 koko 服务器本机上运行 koko device add，或在凭所有者密码登录的浏览器中，为所有者添加设备。",
  "device.revoke.consequenceLastHere":
    "{name} 是所有者的最后一台设备，也是此页面所经由的设备。吊销后，此页面将无法执行任何操作，此设备将删除其密钥，并且只能在 koko 服务器本机上运行 koko device add，或在凭所有者密码登录的浏览器中，为所有者添加设备。",
  "device.revoke.confirm": "吊销",
  "device.revokedDone": "{name} 已吊销。",
  "error.revoked": "此设备的设备会话已被吊销。",
  "error.unknown_device": "当前用户没有此名称的设备。",
  "error.last_device_session": "这是所有者的最后一台设备，吊销前需输入其名称。",
} satisfies Record<keyof typeof en, string>;
