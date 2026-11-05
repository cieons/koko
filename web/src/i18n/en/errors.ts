// The messages for the API's own error codes, which every area shares
// (internal/shared/api/errors.go).
export default {
  "error.invalid_request": "The koko server could not read this request.",
  "error.unknown_field": "This request holds a field the koko server does not know.",
  "error.unknown_operation": "The koko server has no such operation.",
  "error.method_not_allowed": "The koko server takes only POST requests.",
  "error.unsupported_media_type": "The koko server takes only JSON.",
  "error.internal_error": "The koko server failed unexpectedly. Its log says why.",
  "error.invalid_name":
    "A name is 1 to 63 lowercase letters, digits and hyphens, and does not start or end with a hyphen.",
  "error.name_taken": "That name is taken.",
  "error.not_initialised": "The koko server is not initialised yet.",
  "error.local_only": "Only a command on the koko server's own machine can do this.",
  "error.owner_only": "Only the Owner can do this.",
  "error.idempotency_key_reused":
    "This form was sent before with other values. Reload the page and make the change again.",
  "error.request_too_large": "This request is larger than the koko server takes.",
};
