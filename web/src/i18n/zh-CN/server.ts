import type en from "../en/server";

// The server area's messages: its API error codes.
export default {
  "error.already_initialised": "koko 服务器已完成初始化。",
  "error.invalid_address": "该服务器地址无法写入加入码。",
  "error.invalid_web_url": "Web 地址必须是不含路径的 https:// 地址，例如 https://koko.example.com。",
  "error.update_check_failed": "koko 服务器未能查到 koko 的最新版本。",
  "error.upgrade_unsupported": "该 koko 服务器无法就地升级，请按其安装方式升级。",
  "error.upgrade_running": "koko 服务器的升级已在进行中。",
} satisfies Record<keyof typeof en, string>;
