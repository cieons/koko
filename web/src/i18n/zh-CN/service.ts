import type en from "../en/service";

// The service area's messages: its pages and its API error codes.
export default {
  "service.nav": "服务",
  "service.list.title": "服务",
  "service.list.none": "没有符合此视图的服务。",
  "service.filter.label": "筛选服务",
  "service.view.invalid": "此页面地址中的 {parts} 不是服务列表接受的内容。",
  "service.detail.label": "{name} 的详情",
  "service.detail.readOnly": "其主机已退役，因此该服务保持原样，不可修改。",
  "service.edit.button": "编辑",
  "service.edit.title": "编辑 {name}",
  "service.rename.button": "重命名",
  "service.rename.title": "重命名 {name}",
  "service.rename.name": "新名称",
  "service.rename.submit": "重命名",
  "service.remove.button": "删除",
  "service.remove.title": "删除服务 {name}",
  "service.remove.consequence": "{name} 将被永久删除，其端口、属性和备注一并删除。此操作无法撤销。",
  "service.remove.confirm": "删除",
  "error.unknown_service": "该主机上没有使用该名称的服务。",
} satisfies Record<keyof typeof en, string>;
