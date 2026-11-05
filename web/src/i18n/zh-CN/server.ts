import type en from "../en/server";

// The server area's messages: its API error codes.
export default {
  "error.already_initialised": "koko 服务器已经初始化过了。",
  "error.invalid_address": "这个服务器地址无法写入加入码。",
} satisfies Record<keyof typeof en, string>;
