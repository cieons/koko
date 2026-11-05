import type en from "../en/host";

// The host area's messages: its pages and its API error codes.
export default {
  "error.unknown_host": "你能看到的主机里没有叫这个名字的。",
  "error.invalid_value": "某个字段、属性或备注的值不符合它的规则。",
} satisfies Record<keyof typeof en, string>;
