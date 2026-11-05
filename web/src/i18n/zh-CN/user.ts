import type en from "../en/user";

// The user area's messages: its pages and its API error codes.
export default {
  "user.nav": "人员",
  "user.list.title": "人员",
  "user.list.showRemoved": "显示已移除的用户",
  "user.column.name": "名称",
  "user.column.role": "角色",
  "user.column.created": "创建于",
  "user.column.removed": "移除于",
  "user.role.owner": "所有者",
  "user.role.member": "成员",
  "user.remove.button": "移除",
  "user.remove.label": "移除 {name}",
  "user.remove.title": "移除 {name}？",
  "user.remove.consequence":
    "如果 {name} 从未加入，就会被删除，这个名称可以再次使用。否则 {name} 会被永久移除：其设备全部吊销，退出所有空间，其个人空间交给所有者，名称 {name} 永远不会再被使用。",
  "user.remove.confirm": "移除",
  "user.remove.spaceNotEmpty":
    "{name} 从未加入，但其个人空间 {name} 里还有记录。请先移走或删除它们，再移除 {name}。",
  "user.removed.deleted": "{name} 从未加入，因此已被删除，名称 {name} 可以再次使用。",
  "user.removed.removed":
    "{name} 已被移除：其设备已全部吊销，已不在任何空间中。其个人空间 {name} 现在是一个没有成员、只有所有者能看到的空间。",
  "user.invite.title": "邀请用户",
  "user.invite.name": "新用户",
  "user.invite.submit": "邀请",
  "user.invite.done": "已邀请 {name} 成为成员。",
  "error.unknown_user": "没有叫这个名字的用户。",
  "error.user_removed": "这个用户已被移除。",
  "error.owner_not_removable": "所有者永远不能被删除或移除。",
} satisfies Record<keyof typeof en, string>;
