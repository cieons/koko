import type en from "../en/web";

// The web area's messages: its API error codes.
export default {
  "error.cookie_required": "这个浏览器已没有这个页面的有效 cookie。请打开 koko web 打印的地址。",
  "error.signin_required": "请登录后继续。",
  "error.bad_origin": "该请求并非来自本 koko 服务器的页面，已被拒绝。",
  "error.device_only": "此操作仅能在设备上通过 koko web 进行。",
} satisfies Record<keyof typeof en, string>;
