import type en from "../en/service";

// The service area's messages: a Service's panel on its Host's page, and
// the area's API error codes.
export default {
  "service.add": "添加服务",
  "service.on": "{host} 上的服务",
  "service.portsHint": "如 80, 443, 53/udp。",
  "service.portsRule": "每个端口须为 1 至 65535 之间的数字，其后为 /tcp、/udp，或不加后缀表示 TCP。",
  "service.readOnly": "其主机已下线，该服务不可修改。",
  "service.noAttributes": "尚未记录属性。",
  "service.save": "保存",
  "service.added": "已记录 {name}。",
  "service.saved": "已保存 {name}。",
  "service.savedNotRenamed": "除名称外的修改均已保存，名称仍为 {name}。",
  "service.remove": "删除",
  "service.remove.title": "删除 {name}",
  "service.remove.consequence": "{host} 上 {name} 的记录将连同其属性与备注一并删除。主机本身不受影响。",
  "service.remove.done": "已删除 {name}。",
  "error.unknown_service": "该主机上没有使用该名称的服务。",
} satisfies Record<keyof typeof en, string>;
