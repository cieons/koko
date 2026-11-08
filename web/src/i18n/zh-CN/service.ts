import type en from "../en/service";

// The service area's messages: its pages and its API error codes.
export default {
  "service.nav": "服务",
  "service.list.title": "服务",
  "service.list.none": "没有符合条件的服务。",
  "service.view.invalid": "此页地址中的 {parts} 不是服务列表接受的内容。",
  "service.view.reset": "显示默认视图",
  "service.detail.readOnly": "它的主机已退役，所以该服务按原样保留，不能修改。",
  "service.edit.button": "编辑",
  "service.edit.title": "编辑 {name}",
  "service.rename.button": "重命名",
  "service.rename.title": "重命名 {name}",
  "service.rename.name": "新名称",
  "service.rename.submit": "重命名",
  "service.remove.button": "删除",
  "service.remove.title": "删除 {name}？",
  "service.remove.consequence": "{name} 将被永久删除，连同它的端口、属性和备注，无法恢复。",
  "service.remove.confirm": "删除",
  "error.unknown_service": "该主机上没有叫这个名字的服务。",
} satisfies Record<keyof typeof en, string>;
