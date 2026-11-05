import type en from "../en/user";

// The user area's messages: its pages and its API error codes.
export default {
  "error.unknown_user": "没有叫这个名字的用户。",
  "error.user_removed": "这个用户已被移除。",
  "error.owner_not_removable": "所有者永远不能被删除或移除。",
} satisfies Record<keyof typeof en, string>;
