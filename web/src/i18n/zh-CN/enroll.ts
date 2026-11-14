import type en from "../en/enroll";

// The enroll area's messages: its API error codes.
export default {
  "error.unexpected_host_key": "主机提供的主机密钥与 koko 预期的不符，因此 koko 未登录。",
  "error.login_refused": "主机拒绝了其主机账户的密码或密钥。",
  "error.host_unreachable": "koko 无法连接到该主机。",
  "error.key_not_added": "koko 已登录该主机，但未能将 koko 密钥加入其 authorized_keys。",
  "error.already_managed": "该主机已接入；仅在需要固定新的主机密钥时才须再次接入。",
  "error.host_changed": "该主机在接入过程中发生变化，因此 koko 未作任何记录；请再次接入。",
  "error.passphrase_needed": "该私钥已加密，须提供其口令。",
  "error.wrong_passphrase": "该口令无法解开此私钥。",
} satisfies Record<keyof typeof en, string>;
