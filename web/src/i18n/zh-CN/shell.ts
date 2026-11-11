import type en from "../en/shell";

// The shell's messages: the frame every page sits in, and the parts pages
// share.
//
// Product terms (GLOSSARY.md) and the one Simplified Chinese name each takes
// in every catalogue; a term without an entry stays in English:
//
//   Host 主机 · Service 服务 · Attribute 属性 · Preset Attribute 预设属性 ·
//   Note 备注 · Relay 中继 · Registered 已登记 · Enroll 接入 ·
//   Managed 已接入 · Retired 已下线 · Host Account 主机账户 ·
//   Pinned host key 固定的主机密钥 · Secret 机密 · Space 空间 ·
//   Personal Space 个人空间 · User 用户 · Owner 所有者 · Member 成员 ·
//   Invite 邀请 · Removed 已移除 · Join 加入 · Join Code 加入码 ·
//   Device Session 设备会话 · Revoked 已吊销 · Last used 上次使用 ·
//   Password 密码 · Password Link 密码链接 · Sign-in 登录会话 ·
//   koko server koko 服务器 · Server Address 服务器地址 ·
//   Web UI Web 界面 · Web URL Web 地址 · Root Key 根密钥 ·
//   koko key koko 密钥 · Gateway 网关 · Local caller 本机调用方
//
// 已下线 is a Host that is gone, its record kept; 离线, a connection that is
// down, never names it.
//
// Command names (koko web, koko join), names, keys and values are never
// translated.
export default {
  "shell.brand": "koko",
  "shell.documentTitle": "{page} · koko",
  "shell.nav": "主导航",
  "shell.place.hosts": "主机",
  "shell.place.secrets": "机密",
  "shell.place.access": "访问权限",
  "shell.skip": "跳至主要内容",
  "shell.language": "语言",
  "shell.language.current": "语言：{language}",
  "shell.language.en": "English",
  "shell.language.zh-CN": "简体中文",
  "shell.mode": "外观",
  "shell.mode.current": "外观：{mode}",
  "shell.mode.light": "浅色",
  "shell.mode.dark": "深色",
  "shell.mode.system": "跟随系统",
  "shell.account": "账户：{name}",
  "shell.role.owner": "所有者",
  "shell.role.member": "成员",
  "shell.via.device": "经由 {device} 上的 koko web",
  "shell.via.signin": "通过 {client} 登录",
  "shell.via.local": "位于 koko 服务器本机",
  "shell.browser.on": "{system} 上的 {browser}",
  "shell.browser.unknown": "浏览器",
  "shell.loading": "正在加载",
  "shell.pageNotLoaded": "无法加载此页面，请刷新浏览器页面后重试。",
  "shell.view.reset": "重置视图",
  "shell.notFound.title": "页面不存在",
  "shell.notFound.description": "此地址不对应本 koko 服务器的任何页面。",
  "shell.notFound.home": "前往{page}",
  "shell.notice.stopped.title": "koko web 未在运行",
  "shell.notice.stopped.description": "请重新运行 koko web，然后返回此页面。",
  "shell.notice.revoked.title": "此设备的设备会话已被吊销",
  "shell.notice.revoked.description": "此页面已无法执行任何操作。如需在此设备上继续使用 koko，请使用新的加入码重新加入。",
  "shell.error.dropped": "与 koko web 的连接已中断。",
  "shell.error.unexpected": "发生意外错误。",
  "shell.error.unreachable": "无法连接 koko 服务器，请检查网络连接后重试。",
  "shell.retry": "重试",
  "shell.cancel": "取消",
  "shell.close": "关闭",
  "shell.confirm.typeName": "输入 {name} 以确认",
  "shell.copy": "复制",
  "shell.copied": "已复制",
  "shell.copyFailed": "此浏览器不允许复制，请选中文本后自行复制。",
  "shell.shownOnce.timeLeft": "剩余 {time}",
  "shell.shownOnce.notAgain": "此内容仅在此处显示，离开此页面即被丢弃。",
  "shell.openCalendar": "打开日历",
} satisfies Record<keyof typeof en, string>;
