import type en from "../en/device";

// The device area's messages: its pages and its API error codes.
export default {
  "device.nav": "设备",
  "device.list.title": "设备",
  "device.list.showRevoked": "显示已吊销的设备",
  "device.column.user": "用户",
  "device.column.device": "设备",
  "device.column.addedBy": "添加者",
  "device.column.joined": "加入于",
  "device.column.lastUsed": "上次使用",
  "device.column.revoked": "吊销于",
  "device.column.actions": "操作",
  "device.actions.label": "{name} 的操作",
  "device.here": "当前设备",
  "device.addedBy.socket": "koko 服务器本机",
  "device.lastUsed.never": "从未使用",
  "device.revoke.button": "吊销",
  "device.revoke.title": "吊销 {name}",
  "device.revoke.consequence": "{name} 将立即断开，且无法再次连接。由其添加的设备不受影响。",
  "device.revoke.consequenceHere":
    "{name} 是此页面所经由的设备。吊销后，此页面将无法执行任何操作，此设备将删除其密钥。由其添加的设备不受影响。",
  "device.revoke.consequenceLast":
    "{name} 是所有者的最后一台设备。吊销后，只能在 koko 服务器本机上运行 koko device add 为所有者添加设备。",
  "device.revoke.consequenceLastHere":
    "{name} 是所有者的最后一台设备，也是此页面所经由的设备。吊销后，此页面将无法执行任何操作，此设备将删除其密钥，并且只能在 koko 服务器本机上运行 koko device add 为所有者添加设备。",
  "device.revoke.confirm": "吊销",
  "device.add.button": "添加设备",
  "device.add.title": "添加设备",
  "device.add.step": "设备",
  "device.add.user": "用户",
  "device.add.name": "设备名称",
  "device.add.nameHint": "留空时由 koko 自动命名。",
  "device.add.submit": "签发加入码",
  "device.wizard.progress": "进度",
  "device.wizard.done": "完成",
  "device.joinCode.title": "加入码",
  "device.joinCode.intro": "请在 {user} 的新设备 {device} 上运行以下命令：",
  "device.joinCode.expired": "此加入码已过期。请通过“添加设备”签发新的加入码。",
  "error.revoked": "此设备的设备会话已被吊销。",
  "error.unknown_device": "当前用户没有此名称的设备。",
  "error.last_device_session": "这是所有者的最后一台设备，吊销前需输入其名称。",
} satisfies Record<keyof typeof en, string>;
