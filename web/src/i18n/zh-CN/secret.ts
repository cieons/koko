import type en from "../en/secret";

// The secret area's messages: its pages and its API error codes.
export default {
  "error.unknown_secret": "这个空间里没有叫这个名字的机密。",
  "error.value_too_large": "这个机密的值超出了 koko 能保存的大小；koko 从不保存被截断的值。",
  "error.secret_unreadable": "这个机密的密封值无法用根密钥打开：数据库被改动或损坏了。",
} satisfies Record<keyof typeof en, string>;
