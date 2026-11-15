import type en from "../en/passkey";

// The passkey area's messages: signing in with a passkey, making one, the
// passkeys on Account and on a person's panel, and its API error codes.
export default {
  "error.passkey_refused": "通行密钥未被接受。请重试，或改用其他方式登录。",
  "error.unknown_passkey": "不存在该通行密钥。",
} satisfies Record<keyof typeof en, string>;
