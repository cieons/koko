import type en from "../en/service";

// The service area's messages: its pages and its API error codes.
export default {
  "error.unknown_service": "该主机上没有叫这个名字的服务。",
} satisfies Record<keyof typeof en, string>;
