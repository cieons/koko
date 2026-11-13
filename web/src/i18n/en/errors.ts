// The messages for the API's own error codes, which every area shares
// (internal/shared/api/errors.go).
export default {
  "error.invalid_request": "The koko server could not read this request.",
  "error.unknown_field": "This request contains a field the koko server does not recognise.",
  "error.unknown_operation": "The koko server does not offer this operation.",
  "error.method_not_allowed": "The koko server accepts only POST requests.",
  "error.unsupported_media_type": "The koko server accepts only JSON.",
  "error.internal_error": "The koko server failed unexpectedly. Its log records the cause.",
  "error.invalid_name":
    "A name is 1 to 63 lowercase letters, digits and hyphens, and does not start or end with a hyphen. A Secret's name may also contain underscores, and only its first character must be a letter or a digit.",
  "error.name_taken": "This name is already in use.",
  "error.not_initialised": "The koko server is not initialised yet.",
  "error.local_only": "Only a command run on the koko server's own machine can do this.",
  "error.owner_only":
    "Only the Owner or an Administrator can do this, and only the Owner can do it to the Owner or an Administrator.",
  "error.idempotency_key_reused":
    "This form was already sent with other values. Reload the page and make the change again.",
  "error.request_too_large": "This request is larger than the koko server accepts.",
  "error.cli_too_old":
    "This koko is older than the oldest version the koko server works with. Upgrade it with koko update.",
  "error.invalid_batch": "Several of the records are at fault, each for its own reason, so none was changed.",
};
