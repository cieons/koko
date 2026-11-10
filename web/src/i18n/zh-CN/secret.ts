import type en from "../en/secret";

// The secret area's messages: its pages and its API error codes.
export default {
  "error.unknown_secret": "该空间中没有此名称的机密。",
  "error.value_too_large": "该值超出 koko 可保存的大小。koko 不会保存截断的值。",
  "error.secret_unreadable": "此机密的密封值无法用根密钥打开，数据库已被修改或损坏。",
} satisfies Record<keyof typeof en, string>;
