import type en from "../en/errors";

// The messages for the API's own error codes, which every area shares
// (internal/shared/api/errors.go).
export default {
  "error.invalid_request": "koko 服务器无法读取这个请求。",
  "error.unknown_field": "这个请求含有 koko 服务器不认识的字段。",
  "error.unknown_operation": "koko 服务器没有这个操作。",
  "error.method_not_allowed": "koko 服务器只接受 POST 请求。",
  "error.unsupported_media_type": "koko 服务器只接受 JSON。",
  "error.internal_error": "koko 服务器意外出错，原因见它的日志。",
  "error.invalid_name": "名称须为 1 到 63 个小写字母、数字或连字符，且不能以连字符开头或结尾。",
  "error.name_taken": "这个名称已被占用。",
  "error.not_initialised": "koko 服务器尚未初始化。",
  "error.local_only": "只有 koko 服务器本机上的命令才能这样做。",
  "error.owner_only": "只有所有者才能这样做。",
  "error.idempotency_key_reused": "这个表单之前已用其他内容提交过。请重新加载页面，再改一次。",
  "error.request_too_large": "这个请求超出了 koko 服务器能接受的大小。",
} satisfies Record<keyof typeof en, string>;
