import type en from "../en/shell";

// The shell's messages: the frame every page sits in, and the parts pages
// share.
//
// Product terms (GLOSSARY.md) and the one Simplified Chinese name each takes
// in every catalogue; a term without an entry stays in English:
//
//   Host 主机 · Service 服务 · Attribute 属性 · Preset Attribute 预设属性 ·
//   Note 备注 · Relay 中继 · Registered 已登记 · Enroll 纳管 ·
//   Managed 已纳管 · Retired 已退役 · Host Account 主机账户 ·
//   Pinned host key 固定的主机密钥 · Secret 机密 · Space 空间 ·
//   Personal Space 个人空间 · User 用户 · Owner 所有者 · Member 成员 ·
//   Invite 邀请 · Removed 已移除 · Join 加入 · Join Code 加入码 ·
//   Device Session 设备会话 · Revoked 已吊销 · Last used 上次使用 ·
//   Password 密码 · Password Link 密码链接 · Sign-in 登录会话 ·
//   koko server koko 服务器 · Server Address 服务器地址 ·
//   Web UI Web 界面 · Web URL Web 地址 · Root Key 根密钥 ·
//   koko key koko 密钥 · Gateway 网关 · Local caller 本机调用方
//
// Command names (koko web, koko join), names, keys and values are never
// translated.
export default {
  "shell.brand": "koko",
  "shell.nav": "页面",
  "shell.header.device": "{user} · 设备 {device}",
  "shell.header.local": "{user} · koko 服务器本机",
  "shell.account": "账户",
  "shell.language": "语言",
  "shell.language.en": "English",
  "shell.language.zh-CN": "简体中文",
  "shell.mode": "外观",
  "shell.mode.light": "浅色",
  "shell.mode.dark": "深色",
  "shell.mode.system": "跟随系统",
  "shell.loading": "正在加载",
  "shell.pageNotLoaded": "无法加载此页面，请刷新浏览器页面后重试。",
  "shell.notFound.title": "未找到页面",
  "shell.notFound.description": "此地址下没有 Web 界面的页面。",
  "shell.notFound.home": "前往首个页面",
  "shell.notice.stopped.title": "koko web 未在运行",
  "shell.notice.stopped.description": "请重新运行 koko web，然后返回此页面。",
  "shell.notice.revoked.title": "此设备的设备会话已被吊销",
  "shell.notice.revoked.description": "此页面已无法执行任何操作。如需在此设备上继续使用 koko，请使用新的加入码重新加入。",
  "shell.error.dropped": "与 koko web 的连接已中断。",
  "shell.error.unexpected": "发生意外错误。",
  "shell.confirm.cancel": "取消",
  "shell.dialog.cancel": "取消",
  "shell.confirm.typeName": "请输入 {name} 以确认。",
  "shell.shownOnce.copy": "复制",
  "shell.shownOnce.copied": "已复制。",
  "shell.shownOnce.copyFailed": "此浏览器不允许复制，请选中文本后自行复制。",
  "shell.shownOnce.expiresIn": "将于 {time} 后过期。此内容仅在此处显示，离开此页面即被丢弃。",
} satisfies Record<keyof typeof en, string>;
