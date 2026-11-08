import type en from "../en/web";

// The web area's messages: its API error codes.
export default {
  "error.cookie_required": "这个浏览器已没有这个页面的有效 cookie。请打开 koko web 打印的地址。",
} satisfies Record<keyof typeof en, string>;
