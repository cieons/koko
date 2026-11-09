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
  "device.here": "（这台设备）",
  "device.addedBy.socket": "koko 服务器本机",
  "device.lastUsed.never": "从未",
  "device.revoke.button": "吊销",
  "device.revoke.label": "吊销 {name}",
  "device.revoke.title": "吊销 {name}？",
  "device.revoke.consequence": "{name} 会立即被切断，再也无法连接。它添加的设备仍可继续使用。",
  "device.revoke.consequenceHere":
    "{name} 就是这个页面所经由的设备：吊销后，这个页面将无法再做任何事，这台设备也会删除它的密钥。它添加的设备仍可继续使用。",
  "device.revoke.consequenceLast":
    "{name} 是所有者的最后一台设备：吊销后，只有在 koko 服务器本机上运行 koko device add 才能为所有者添加设备。",
  "device.revoke.consequenceLastHere":
    "{name} 是所有者的最后一台设备，也是这个页面所经由的设备：吊销后，这个页面将无法再做任何事，这台设备会删除它的密钥，并且只有在 koko 服务器本机上运行 koko device add 才能为所有者添加设备。",
  "device.revoke.confirm": "吊销",
  "device.add.title": "添加设备",
  "device.add.user": "用户",
  "device.add.name": "设备名称（可选）",
  "device.add.submit": "添加设备",
  "device.joinCode.title": "加入码",
  "device.joinCode.intro": "在 {user} 的新设备 {device} 上运行这一行：",
  "device.joinCode.expired": "这个加入码已过期。请再添加一次设备以获得新的加入码。",
  "error.revoked": "这台设备的设备会话已被吊销。",
  "error.unknown_device": "你没有叫这个名字的设备。",
  "error.last_device_session": "这是所有者的最后一台设备，吊销它需要输入它的名称。",
} satisfies Record<keyof typeof en, string>;
