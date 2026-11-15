import type en from "../en/identity";

// The identity area's messages: the Identity provider panel over Access,
// and its API error codes.
export default {
  "error.invalid_provider": "身份提供方的某项设置无效，或其签发者的发现文档无效。",
  "error.provider_unreachable": "身份提供方未在规定时间内响应，请稍后重试。",
  "error.provider_unset": "尚未配置身份提供方。",
} satisfies Record<keyof typeof en, string>;
