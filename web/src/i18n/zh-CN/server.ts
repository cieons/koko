import type en from "../en/server";

// The server area's messages: its API error codes.
export default {
  "error.already_initialised": "koko 服务器已完成初始化。",
  "error.invalid_address": "该服务器地址无法写入加入码。",
  "error.invalid_web_url": "Web 地址必须是不含路径的 https:// 地址，例如 https://koko.example.com。",
} satisfies Record<keyof typeof en, string>;
