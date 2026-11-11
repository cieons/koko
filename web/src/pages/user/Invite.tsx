import { Description, Label, Radio, RadioGroup } from "@heroui/react";
import { type ReactNode, useState } from "react";

import type { JoinCode } from "../../api/device";
import type { PasswordLink } from "../../api/signin";
import { type Space, SpaceMemberAdd } from "../../api/space";
import { UserInvite } from "../../api/user";
import { call } from "../../client";
import { useWrite } from "../../shell/data";
import { errorMessage, useLanguage, useT } from "../../shell/i18n";
import { useShownOnce } from "../../shell/shownOnce";
import { Field, SearchChoices } from "../../ui/fields";
import { AlertLine } from "../../ui/states";
import {
  defaultValidFor,
  IssueDialog,
  JoinCodeLine,
  PasswordLinkLine,
  type ValidFor,
  ValidForChoice,
  validForRequest,
} from "../device/IssueDialog";
import { useWebURL } from "../signin/webURL";

/** Method is how the invited person will use koko: a Join Code for the CLI, or a Password Link for a browser. */
type Method = "terminal" | "browser";

/** defaultMethod is the Method an Invite starts at. */
const defaultMethod: Method = "terminal";

/** Issued is what an Invite issued, shown once: a Join Code or a Password Link. */
type Issued = { joinCode: JoinCode } | { link: PasswordLink };

/**
 * Invited is who Invite invited, as it was sent, and the Spaces it did not
 * make them a Member of, with why.
 */
interface Invited {
  user: string;
  method: Method;
  validFor: ValidFor;
  notAdded: string[];
  reason?: unknown;
}

/**
 * Invite invites a User in two steps (docs/web-design.md, "Invite"): their
 * name; how they will use koko, in a terminal (a Join Code) or in a
 * browser (a Password Link, which needs the Web URL, from server.show, and
 * is disabled with the reason while it is unset); Valid for; and the shared
 * Spaces to make them a Member of, a searchable choice of several; then
 * the code or link, shown once. It calls user.invite, then
 * space.member.add once per Space chosen. A membership call that fails
 * leaves the Invite made, so the second step still shows the code or link,
 * which cannot be shown again, and names each Space they were not added
 * to. It never asks first.
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
  const [method, setMethod] = useState<Method>(defaultMethod);
  const [validFor, setValidFor] = useState<ValidFor>(defaultValidFor);
  const [spaces, setSpaces] = useState<string[]>([]);
  const [adding, setAdding] = useState(false);
  const [invited, setInvited] = useState<Invited>();
  // The code or link alone is shown once; once the browser forgets it, the second step says so.
  const [issued, setIssued] = useShownOnce<Issued>();
  const invite = useWrite(UserInvite);
  const webURL = useWebURL();

  const submit = async () => {
    const sent = { method, validFor };
    const answer = await invite.submit({
      name,
      ...validForRequest(validFor),
      ...(method === "browser" ? { web: true } : {}),
    });
    const shown: Issued | undefined = answer?.join_code
      ? { joinCode: answer.join_code }
      : answer?.password_link && { link: answer.password_link };
    if (answer === undefined || shown === undefined) {
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
    setIssued(shown);
    setInvited({ user: answer.user.name, ...sent, notAdded, reason });
    onInvited();
  };

  return (
    <IssueDialog
      title={t("user.invite")}
      lead={t("user.invite.lead")}
      submit={t("user.invite")}
      pending={invite.pending || adding}
      changed={name !== "" || method !== defaultMethod || validFor !== defaultValidFor || spaces.length > 0}
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
            {issued === undefined ? (
              <p>
                {t(invited.method === "browser" ? "user.invite.linkForgotten" : "user.invite.forgotten", {
                  name: invited.user,
                })}
              </p>
            ) : "joinCode" in issued ? (
              <JoinCodeLine
                joinCode={issued.joinCode}
                validFor={invited.validFor}
                expired={t("user.invite.expired", { name: invited.user })}
              />
            ) : (
              <PasswordLinkLine
                link={issued.link}
                validFor={invited.validFor}
                expired={t("user.invite.linkExpired", { name: invited.user })}
              />
            )}
          </>
        )
      }
      onSubmit={() => void submit()}
      onClose={() => {
        setIssued(undefined);
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
      <RadioGroup value={method} onChange={(m) => setMethod(m as Method)}>
        <Label>{t("user.invite.method")}</Label>
        <div className="mt-1 grid gap-3 sm:grid-cols-2">
          <MethodCard value="terminal" label={t("user.invite.terminal")} hint={t("user.invite.terminalHint")} />
          <MethodCard
            value="browser"
            label={t("user.invite.browser")}
            isDisabled={!webURL.set}
            hint={webURL.reason?.(t) ?? t("user.invite.browserHint")}
          />
        </div>
      </RadioGroup>
      <ValidForChoice value={validFor} onChange={setValidFor} />
      {shared.length > 0 && (
        <SearchChoices
          label={t("user.invite.spaces")}
          values={spaces}
          onChange={setSpaces}
          options={shared.map((s) => ({ id: s.name, label: s.name }))}
          placeholder={t("user.invite.spacesPlaceholder")}
          searchLabel={t("space.access.search.spaces")}
          noMatch={t("space.access.noSpace")}
          description={t("user.invite.spacesHint")}
        />
      )}
    </IssueDialog>
  );
}

/** MethodCard is one way an invited person will use koko, as a radio card with what it issues under it. */
function MethodCard({
  value,
  label,
  hint,
  isDisabled = false,
}: {
  value: Method;
  label: string;
  hint: ReactNode;
  isDisabled?: boolean;
}) {
  return (
    <Radio
      value={value}
      isDisabled={isDisabled}
      className="rounded-2xl border border-border p-4 data-[selected=true]:border-accent data-[selected=true]:bg-accent-soft"
    >
      <Radio.Content>
        <Radio.Control>
          <Radio.Indicator />
        </Radio.Control>
        {label}
      </Radio.Content>
      <Description>{hint}</Description>
    </Radio>
  );
}
