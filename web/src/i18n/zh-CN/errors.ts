import type en from "../en/errors";

// The messages for the API's own error codes, which every area shares
// (internal/shared/api/errors.go).
export default {
  "error.invalid_request": "koko 服务器无法读取此请求。",
  "error.unknown_field": "此请求含有 koko 服务器无法识别的字段。",
  "error.unknown_operation": "koko 服务器不提供此操作。",
  "error.method_not_allowed": "koko 服务器仅接受 POST 请求。",
  "error.unsupported_media_type": "koko 服务器仅接受 JSON。",
  "error.internal_error": "koko 服务器发生意外错误，原因记录在其日志中。",
  "error.invalid_name":
    "名称须为 1 至 63 个小写字母、数字或连字符，且不得以连字符开头或结尾。机密的名称还可包含下划线，仅首字符须为字母或数字。",
  "error.name_taken": "此名称已被占用。",
  "error.not_initialised": "koko 服务器尚未初始化。",
  "error.local_only": "此操作仅能由 koko 服务器本机上运行的命令执行。",
  "error.owner_only": "此操作仅限所有者执行。",
  "error.idempotency_key_reused": "此表单此前已以其他内容提交。请重新加载页面，然后再次修改。",
  "error.request_too_large": "此请求超出 koko 服务器可接受的大小。",
  "error.cli_too_old": "此 koko 的版本低于 koko 服务器支持的最低版本，需通过 koko update 升级。",
} satisfies Record<keyof typeof en, string>;
