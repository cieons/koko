import type en from "../en/files";

// The files area's messages: the API error codes of the web terminal's
// files endpoints.
export default {
  "error.unknown_tunnel": "此 SSH 会话已不再开启。请打开新的终端以访问主机。",
  "error.ssh_session_ended": "此 SSH 会话已结束，其文件传输随之终止。",
  "error.sftp_unavailable": "此主机未提供 SFTP，koko 无法通过它传输文件。",
  "error.host_refused": "主机拒绝了该请求。",
  "error.file_exists": "此目录中已存在同名文件。",
  "error.bad_offset": "上传的部分内容未能送达。请重新上传该文件。",
  "error.unknown_upload": "此上传已不在进行中。",
  "error.not_a_file": "仅能下载普通文件。",
  "error.unknown_download": "此下载链接已过期或已被使用。请重新下载该文件。",
  "error.too_many_uploads": "此 SSH 会话同时进行的上传已达上限。请等待其中一项完成。",
} satisfies Record<keyof typeof en, string>;
