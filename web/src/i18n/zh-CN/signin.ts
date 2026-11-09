import type en from "../en/signin";

// The signin area's messages: its API error codes.
export default {
  "error.web_url_unset": "签发密码链接需要 Web URL，但尚未设置。所有者可在 koko 服务器所在机器上执行 koko server set web_url= 进行设置。",
  "error.invalid_password": "密码长度须为 15 至 128 个字符，且不得与用户名相同。字符种类不限。",
  "error.wrong_password": "用户名或密码不正确。",
  "error.too_many_attempts": "失败次数过多，请稍后重试，最长需等待 15 分钟。",
  "error.link_used": "此密码链接已被使用，请联系所有者重新签发。",
  "error.link_invalid": "此密码链接已过期或已失效，请联系所有者重新签发。",
  "error.unknown_signin": "不存在该 ID 的登录会话。",
  "error.no_password": "该用户尚未设置密码。",
} satisfies Record<keyof typeof en, string>;
