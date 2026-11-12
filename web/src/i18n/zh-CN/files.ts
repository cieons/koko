import type en from "../en/files";

// The files area's messages: the API error codes of the web terminal's
// files endpoints.
export default {
  "error.unknown_tunnel": "此 SSH 会话已不再开启。请打开新的终端以访问主机。",
  "error.ssh_session_ended": "此 SSH 会话已结束，其文件传输随之终止。",
  "error.sftp_unavailable": "此主机未提供 SFTP，koko 无法通过它传输文件。",
  "error.host_refused": "主机拒绝了该请求。",
} satisfies Record<keyof typeof en, string>;
