import type en from "../en/space";

// The space area's messages: its pages and its API error codes.
export default {
  "space.nav": "空间",
  "space.list.title": "空间",
  "error.unknown_space": "你能使用的空间里没有叫这个名字的。",
  "error.personal_space": "个人空间不能重命名、删除或添加成员。",
  "error.space_not_empty": "这个空间里还有记录，请先移走或删除它们。",
} satisfies Record<keyof typeof en, string>;
