import type en from "../en/enroll";

// The enroll area's messages: its API error codes.
export default {
  "error.unexpected_host_key": "主机提供的主机密钥与 koko 预期的不符，因此 koko 未登录。",
  "error.login_refused": "主机拒绝了其主机账户的密码或密钥。",
  "error.host_unreachable": "koko 无法连接到该主机。",
  "error.key_not_added": "koko 已登录该主机，但未能将 koko 密钥加入其 authorized_keys。",
  "error.already_managed": "该主机已纳管；仅在需要固定新的主机密钥时才须再次纳管。",
  "error.host_changed": "该主机在纳管过程中发生变化，因此 koko 未作任何记录；请再次纳管。",
} satisfies Record<keyof typeof en, string>;
