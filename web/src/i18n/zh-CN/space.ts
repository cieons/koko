import type en from "../en/space";

// The space area's messages: its pages and its API error codes.
export default {
  "error.unknown_space": "在可使用的空间中找不到此名称。",
  "error.personal_space": "个人空间不能重命名、删除或添加成员。",
  "error.space_not_empty": "此空间仍包含记录，请先移走或删除这些记录。",
} satisfies Record<keyof typeof en, string>;
