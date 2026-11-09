// The user area's messages: its pages and its API error codes.
export default {
  "user.nav": "People",
  "user.list.title": "People",
  "user.list.showRemoved": "Show Removed Users",
  "user.column.name": "Name",
  "user.column.role": "Role",
  "user.column.created": "Created",
  "user.column.removed": "Removed",
  "user.column.actions": "Actions",
  "user.actions.label": "Actions for {name}",
  "user.role.owner": "Owner",
  "user.role.member": "Member",
  "user.remove.button": "Remove",
  "user.remove.title": "Remove {name}",
  "user.remove.consequence":
    "If {name} never joined, the User is deleted and the name becomes available again. Otherwise {name} is Removed permanently: their devices are revoked, they leave every Space, their Personal Space passes to the Owner, and the name {name} cannot be used again.",
  "user.remove.confirm": "Remove",
  "user.remove.spaceNotEmpty":
    "{name} never joined, and the Personal Space {name} still holds records. Move or remove those records, then remove {name} again.",
  "user.removed.deleted": "{name} never joined and was deleted. The name {name} is available again.",
  "user.removed.removed":
    "{name} was Removed. Their devices are revoked and they belong to no Space. The Personal Space {name} now has no Members, and only the Owner can see it.",
  "user.invite.button": "Invite User",
  "user.invite.title": "Invite User",
  "user.invite.step": "User",
  "user.invite.name": "Name",
  "user.invite.nameHint": "The new User joins as a Member.",
  "user.invite.submit": "Invite",
  "user.invite.done": "{name} was invited as a Member.",
  "error.unknown_user": "No User has that name.",
  "error.user_removed": "That User was Removed.",
  "error.owner_not_removable": "The Owner cannot be deleted or Removed.",
};
