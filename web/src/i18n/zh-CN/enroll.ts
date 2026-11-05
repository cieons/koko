import type en from "../en/enroll";

// The enroll area's messages: its API error codes.
export default {
  "error.unexpected_host_key": "主机提供的主机密钥与 koko 预期的不同，所以 koko 没有登录。",
  "error.login_refused": "主机拒绝了为其主机账户提供的密码或密钥。",
  "error.host_unreachable": "koko 无法连接到该主机。",
  "error.key_not_added": "koko 已登录该主机，但无法把 koko 密钥加入它的 authorized_keys。",
  "error.already_managed": "该主机已是受管状态；只有在要固定新的主机密钥时才需再次纳管。",
  "error.host_changed": "该主机在纳管过程中发生了变化，所以 koko 什么也没有记录；请再次纳管。",
} satisfies Record<keyof typeof en, string>;
