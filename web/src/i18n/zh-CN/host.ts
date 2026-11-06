import type en from "../en/host";

// The host area's messages: its pages and its API error codes.
export default {
  "error.unknown_host": "你能看到的主机里没有叫这个名字的。",
  "error.invalid_value": "某个字段、属性或备注的值不符合它的规则。",
  "error.host_was_managed": "该主机曾被纳管，所以 koko 保留其记录，永不删除。",
  "error.invalid_batch": "其中多台主机各有各的问题，所以一台也没有添加。",
  "error.host_retired": "该主机已退役：只能修改其属性和备注，或将其移到别的空间。",
} satisfies Record<keyof typeof en, string>;
