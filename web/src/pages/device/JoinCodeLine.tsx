import type { JoinCode } from "../../api/device";
import { useT } from "../../shell/i18n";
import { ShownOnceLine } from "../../shell/ShownOnceLine";

/**
 * joinCodeValidFor is how long a Join Code works from its issue, in seconds,
 * when the request names no valid_for: 15 minutes, the koko server's
 * default (#115).
 */
const joinCodeValidFor = 15 * 60;

/**
 * JoinCodeLine shows a Join Code just issued, by an Invite or an added
 * device, once: the koko join line to paste on the new device, with copy
 * and its countdown.
 */
export function JoinCodeLine({ joinCode }: { joinCode: JoinCode }) {
  const t = useT();
  return (
    <ShownOnceLine
      label={t("device.joinCode.title")}
      intro={t("device.joinCode.intro", { user: joinCode.user, device: joinCode.device })}
      line={`koko join ${joinCode.code}`}
      validFor={joinCodeValidFor}
      expired={t("device.joinCode.expired")}
    />
  );
}
