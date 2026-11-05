import type en from "../en/space";

// The space area's messages: its pages and its API error codes.
export default {
  "space.nav": "空间",
  "space.list.title": "空间",
  "space.personal": "个人",
  "space.add.name": "新空间",
  "space.add.submit": "添加空间",
  "space.detail.personal": "个人空间",
  "space.detail.shared": "共享空间",
  "space.detail.members": "成员",
  "space.detail.noMembers": "没有成员。",
  "space.detail.holds": "其中的记录",
  "space.counts.hosts": "主机",
  "space.counts.retiredHosts": "已退役的主机",
  "space.counts.secrets": "机密",
  "space.member.remove": "移出",
  "space.member.remove.label": "把 {user} 移出 {space}",
  "space.member.add.user": "用户",
  "space.member.add.choose": "选择一个用户",
  "space.member.add.submit": "添加成员",
  "space.rename.name": "新名称",
  "space.rename.submit": "重命名",
  "space.remove.button": "删除空间",
  "space.remove.title": "删除 {name}？",
  "space.remove.consequence":
    "{name} 将连同它的成员关系被永久删除。仍有主机、已退役的主机或机密的空间会被拒绝删除。",
  "space.remove.confirm": "删除",
  "error.unknown_space": "你能使用的空间里没有叫这个名字的。",
  "error.personal_space": "个人空间不能重命名、删除或添加成员。",
  "error.space_not_empty": "这个空间里还有记录，请先移走或删除它们。",
} satisfies Record<keyof typeof en, string>;
