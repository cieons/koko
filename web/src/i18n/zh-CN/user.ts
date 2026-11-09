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
  "user.column.actions": "操作",
  "user.actions.label": "{name} 的操作",
  "user.role.owner": "所有者",
  "user.role.member": "成员",
  "user.remove.button": "移除",
  "user.remove.title": "移除 {name}",
  "user.remove.consequence":
    "如果 {name} 从未加入，该用户将被删除，名称可再次使用。否则 {name} 将被永久移除：其设备全部吊销，退出所有空间，其个人空间转交所有者，名称 {name} 不可再次使用。",
  "user.remove.confirm": "移除",
  "user.remove.spaceNotEmpty": "{name} 从未加入，但个人空间 {name} 仍包含记录。请先移走或删除这些记录，再移除 {name}。",
  "user.removed.deleted": "{name} 从未加入，已被删除。名称 {name} 可再次使用。",
  "user.removed.removed":
    "{name} 已被移除，其设备已全部吊销，且不再属于任何空间。个人空间 {name} 现已没有成员，仅所有者可见。",
  "user.invite.button": "邀请用户",
  "user.invite.title": "邀请用户",
  "user.invite.step": "用户",
  "user.invite.name": "名称",
  "user.invite.nameHint": "新用户将以成员身份加入。",
  "user.invite.submit": "邀请",
  "user.invite.done": "已邀请 {name} 成为成员。",
  "error.unknown_user": "没有此名称的用户。",
  "error.user_removed": "该用户已被移除。",
  "error.owner_not_removable": "所有者不能被删除或移除。",
} satisfies Record<keyof typeof en, string>;
