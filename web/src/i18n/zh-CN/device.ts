import type en from "../en/device";

// The device area's messages: its pages and its API error codes.
export default {
  "error.revoked": "这台设备的设备会话已被吊销。",
  "error.unknown_device": "你没有叫这个名字的设备。",
  "error.last_device_session": "这是所有者的最后一台设备，吊销它需要输入它的名称。",
} satisfies Record<keyof typeof en, string>;
