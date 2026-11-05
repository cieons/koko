import type en from "../en/shell";

// The shell's messages: the frame every page sits in.
export default {
  "shell.brand": "koko",
  "shell.nav": "页面",
  "shell.header.device": "{user} · 设备 {device}",
  "shell.header.local": "{user} · koko 服务器本机",
  "shell.language": "语言",
  "shell.language.en": "English",
  "shell.language.zh-CN": "简体中文",
  "shell.loading": "加载中…",
  "shell.notFound": "这里没有页面。",
  "shell.notice.stopped": "koko web 没有在运行。请重新运行 koko web，然后回到这个页面。",
  "shell.notice.revoked":
    "这台设备的设备会话已被吊销，这个页面无法再做任何事。要在这台设备上再次使用 koko，请用新的加入码让它重新加入。",
  "shell.error.dropped": "与 koko web 的连接断开了。",
  "shell.error.unexpected": "发生了意外错误。",
  "shell.confirm.cancel": "取消",
  "shell.confirm.typeName": "输入 {name} 以确认。",
} satisfies Record<keyof typeof en, string>;
