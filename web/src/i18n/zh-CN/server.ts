import type en from "../en/server";

// The server area's messages: the release notice and the koko server's line
// in the account panel, which the shell shows the Owner, and its API error
// codes.
export default {
  "server.release.region": "koko 新版本",
  "server.release.available": "koko {latest} 已发布。",
  "server.release.runs": "该 koko 服务器运行的版本为 {version}。",
  "server.release.notes": "发布说明",
  "server.release.later": "稍后",
  "server.release.howTo": "升级方法",
  "server.release.image.setVersion": "在 compose.yaml 所在目录的 .env 中设置版本。若 .env 已设置 KOKO_VERSION=latest，则跳过此步。",
  "server.release.image.setVersionLabel": ".env 中的设置",
  "server.release.image.pull": "随后在同一目录中拉取新镜像，并重新启动容器：",
  "server.release.image.pullLabel": "Compose 命令",
  "server.release.none": "该 koko 服务器为手动启动。请将 kokod 替换为 {latest} 版本，然后重新启动。",
  "server.line.label": "koko 服务器",
  "server.line.checkedAgo": "上次检查：{ago}",
  "server.line.notChecked": "尚未检查",
  "server.line.failed": "上次检查失败：{error}",
  "server.check.now": "立即检查",
  "server.check.checking": "正在检查新版本…",
  "server.check.newest": "{version} 已是最新版本。",
  "error.already_initialised": "koko 服务器已完成初始化。",
  "error.invalid_address": "该服务器地址无法写入加入码。",
  "error.invalid_web_url": "Web 地址必须是不含路径的 https:// 地址，例如 https://koko.example.com。",
  "error.update_check_failed": "koko 服务器未能查到 koko 的最新版本。",
  "error.upgrade_unsupported": "该 koko 服务器无法就地升级，请按其安装方式升级。",
  "error.upgrade_running": "koko 服务器的升级已在进行中。",
} satisfies Record<keyof typeof en, string>;
