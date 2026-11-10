import type en from "../en/host";

// The host area's messages: a Host's state words, which the shared
// StateMark shows, and its API error codes; its pages add theirs.
export default {
  "host.state.managed": "已接入",
  "host.state.registered": "已登记",
  "host.state.retired": "已下线",
  "error.unknown_host": "没有可查看的主机使用该名称。",
  "error.invalid_value": "某个字段、属性或备注的值不符合其规则。",
  "error.host_was_managed": "该主机曾经接入，因此 koko 保留其记录，不予删除。",
  "error.invalid_batch": "多台主机各有错误，因此均未添加。",
  "error.host_retired": "该主机已下线，仅可修改其属性和备注，或移至其他空间。",
  "error.invalid_relay": "该主机不能在此作为中继：中继须为另一台已接入的主机，且仅限一跳。",
  "error.relay_in_use": "该主机是一台未下线主机的中继，因此暂不能下线。",
} satisfies Record<keyof typeof en, string>;
