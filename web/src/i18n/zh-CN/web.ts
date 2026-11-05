import type en from "../en/web";

// The web area's messages: its API error codes.
export default {
  "error.cookie_required": "这个页面的访问权限已随打开它的那次 koko web 结束。请打开 koko web 打印的地址。",
} satisfies Record<keyof typeof en, string>;
