import { Checkbox, CheckboxGroup, Description, Label } from "@heroui/react";
import { useState } from "react";

import type { JoinCode } from "../../api/device";
import { type Space, SpaceMemberAdd } from "../../api/space";
import { UserInvite } from "../../api/user";
import { call } from "../../client";
import { useWrite } from "../../shell/data";
import { errorMessage, useLanguage, useT } from "../../shell/i18n";
import { useShownOnce } from "../../shell/shownOnce";
import { Field } from "../../ui/fields";
import { AlertLine } from "../../ui/states";
import { IssueDialog, JoinCodeLine } from "../device/IssueDialog";

/** Invited is who Invite invited, and the Spaces it did not make them a Member of, with why. */
interface Invited {
  user: string;
  notAdded: string[];
  reason?: unknown;
}

/**
 * Invite invites a User in two steps (docs/web-design.md, "Invite"): their
 * name and the shared Spaces to make them a Member of; then their Join Code,
 * shown once. It calls user.invite, then space.member.add once per Space
 * chosen. A membership call that fails leaves the Invite made, so the
 * second step still shows the Join Code, which cannot be shown again, and
 * names each Space they were not added to. It never asks first.
 */
export function Invite({
  shared,
  onInvited,
  onClose,
}: {
  shared: readonly Space[];
  onInvited: () => void;
  onClose: () => void;
}) {
  const t = useT();
  const language = useLanguage();
  const [name, setName] = useState("");
  const [spaces, setSpaces] = useState<string[]>([]);
  const [adding, setAdding] = useState(false);
  const [invited, setInvited] = useState<Invited>();
  // The Join Code alone is shown once; once the browser forgets it, the second step says so.
  const [joinCode, setJoinCode] = useShownOnce<JoinCode>();
  const invite = useWrite(UserInvite);

  const submit = async () => {
    const answer = await invite.submit({ name });
    if (answer?.join_code === undefined) {
      return;
    }
    setAdding(true);
    const notAdded: string[] = [];
    let reason: unknown;
    for (const space of spaces) {
      try {
        await call(SpaceMemberAdd, { space, users: [answer.user.name] });
      } catch (failure) {
        notAdded.push(space);
        reason ??= failure;
      }
    }
    setAdding(false);
    setJoinCode(answer.join_code);
    setInvited({ user: answer.user.name, notAdded, reason });
    onInvited();
  };

  return (
    <IssueDialog
      title={t("user.invite")}
      lead={t("user.invite.lead")}
      submit={t("user.invite")}
      pending={invite.pending || adding}
      error={invite.error}
      doneTitle={t("user.invite.done", { name: invited?.user ?? name })}
      issued={
        invited && (
          <>
            {invited.notAdded.length > 0 && (
              <AlertLine>
                {t("user.invite.notAdded", {
                  name: invited.user,
                  spaces: new Intl.ListFormat(language, { type: "conjunction" }).format(invited.notAdded),
                  reason: errorMessage(t, invited.reason),
                })}
              </AlertLine>
            )}
            {joinCode ? (
              <JoinCodeLine joinCode={joinCode} expired={t("user.invite.expired", { name: invited.user })} />
            ) : (
              <p>{t("user.invite.forgotten", { name: invited.user })}</p>
            )}
          </>
        )
      }
      onSubmit={() => void submit()}
      onClose={() => {
        setJoinCode(undefined);
        onClose();
      }}
    >
      <Field
        label={t("user.invite.name")}
        value={name}
        onChange={setName}
        description={t("user.invite.nameHint")}
        code
        autoFocus
        validate={(v) => (v === "" ? t("user.invite.nameRequired") : undefined)}
      />
      {shared.length > 0 && (
        <CheckboxGroup value={spaces} onChange={setSpaces}>
          <Label>{t("user.invite.spaces")}</Label>
          <Description>{t("user.invite.spacesHint")}</Description>
          <div className="mt-1 flex flex-wrap gap-x-6 gap-y-2">
            {shared.map((s) => (
              <Checkbox key={s.id} value={s.name}>
                <Checkbox.Content>
                  <Checkbox.Control>
                    <Checkbox.Indicator />
                  </Checkbox.Control>
                  {s.name}
                </Checkbox.Content>
              </Checkbox>
            ))}
          </div>
        </CheckboxGroup>
      )}
    </IssueDialog>
  );
}
