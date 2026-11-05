import type en from "../en/user";

// The user area's messages: its pages and its API error codes.
export default {
  "error.unknown_user": "没有叫这个名字的用户。",
} satisfies Record<keyof typeof en, string>;
