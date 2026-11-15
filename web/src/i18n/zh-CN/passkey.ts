import type en from "../en/passkey";

// The passkey area's messages: signing in with a passkey, making one, the
// passkeys on Account and on a person's panel, and its API error codes.
export default {
  "passkey.signIn": "使用通行密钥登录",
  "passkey.link.title": "设置登录方式",
  "passkey.link.create": "创建通行密钥",
  "passkey.link.password": "改为设置密码",
  "passkey.link.back": "返回",
  "passkey.title": "通行密钥",
  "passkey.none": "尚未添加通行密钥。",
  "passkey.madeFor": "为 {host} 创建",
  "passkey.neverUsed": "从未使用",
  "passkey.synced": "已同步",
  "passkey.count.one": "{count} 个通行密钥",
  "passkey.count.other": "{count} 个通行密钥",
  "passkey.add": "添加通行密钥",
  "passkey.add.name": "名称",
  "passkey.add.nameRequired": "请输入名称。",
  "passkey.add.nameTooLong": "名称不得超过 64 个字符。",
  "passkey.add.nameInvalid": "名称长度须为 1 至 64 个字符，且不得包含控制字符。",
  "passkey.add.done": "已添加通行密钥 {name}。",
  "passkey.viaBrowser": "通行密钥须在经由 Web 地址登录的浏览器中添加。",
  "passkey.needsDomain": "通行密钥要求 Web 地址的主机为域名，而非 IP 地址。",
  "passkey.signedIn": "当前浏览器已以 {name} 的身份登录。",
  "passkey.exists": "此设备或密码管理器中已存有该用户的通行密钥。",
  "passkey.failed": "浏览器未能完成通行密钥操作。请重试，或改用其他方式。",
  "passkey.remove": "移除",
  "passkey.remove.label": "移除通行密钥 {name}",
  "passkey.remove.title": "移除通行密钥 {name}",
  "passkey.remove.consequence":
    "凭该通行密钥登录的浏览器将退出登录。存有该通行密钥的设备或密码管理器仍会保留它，请同时在其设置中将其移除。",
  "passkey.remove.consequenceHere":
    "当前浏览器凭该通行密钥登录，将随之退出登录，凭其登录的其他浏览器亦同。存有该通行密钥的设备或密码管理器仍会保留它，请同时在其设置中将其移除。",
  "passkey.remove.consequenceLast":
    "凭该通行密钥登录的浏览器将退出登录。这是 {name} 仅剩的登录方式，移除后其下次登录须使用登录链接。存有该通行密钥的设备或密码管理器仍会保留它，请同时在其设置中将其移除。",
  "passkey.remove.consequenceHereLast":
    "当前浏览器凭该通行密钥登录，将随之退出登录，凭其登录的其他浏览器亦同。这是 {name} 仅剩的登录方式，移除后其下次登录须使用登录链接。存有该通行密钥的设备或密码管理器仍会保留它，请同时在其设置中将其移除。",
  "passkey.remove.done": "已移除通行密钥 {name}。",
  "passkey.promote.title": "通行密钥",
  "passkey.promote.byLink": "{passkey}，经由 {by} 签发的登录链接创建",
  "passkey.promote.own": "{passkey}，由 {name} 在已登录的浏览器中添加",
  "passkey.promote.none": "无通行密钥。",
  "error.passkey_refused": "通行密钥未被接受。请重试，或改用其他方式登录。",
  "error.unknown_passkey": "不存在该通行密钥。",
} satisfies Record<keyof typeof en, string>;
