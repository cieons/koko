// The user area's messages: its pages and its API error codes.
export default {
  "user.nav": "People",
  "user.list.title": "People",
  "user.list.showRemoved": "Show Removed Users",
  "user.column.name": "Name",
  "user.column.role": "Role",
  "user.column.created": "Created",
  "user.column.removed": "Removed",
  "user.role.owner": "Owner",
  "user.role.member": "Member",
  "user.remove.button": "Remove",
  "user.remove.label": "Remove {name}",
  "user.remove.title": "Remove {name}?",
  "user.remove.consequence":
    "If {name} never joined, they are deleted and the name is free again. Otherwise they are Removed for good: their devices are revoked, they leave every Space, their Personal Space passes to the Owner, and the name {name} is never used again.",
  "user.remove.confirm": "Remove",
  "user.remove.spaceNotEmpty":
    "{name} never joined, and their Personal Space {name} still holds records. Move or remove them first, then remove {name}.",
  "user.removed.deleted": "{name} never joined, so they were deleted, and the name {name} is free again.",
  "user.removed.removed":
    "{name} was Removed: their devices are revoked and they are in no Space. Their Personal Space {name} is now a Space with no Members that only the Owner sees.",
  "user.invite.title": "Invite a User",
  "user.invite.name": "New User",
  "user.invite.submit": "Invite",
  "user.invite.done": "Invited {name} as a Member.",
  "error.unknown_user": "No User has that name.",
  "error.user_removed": "That User was Removed.",
  "error.owner_not_removable": "The Owner can never be deleted or Removed.",
};
