import { Button, Chip, Switch } from "@heroui/react";
import { Laptop, Plus } from "lucide-react";
import { useId, useState } from "react";

import { DeviceAdd, DeviceList, DeviceRevoke, type DeviceSession, type JoinCode } from "../../api/device";
import { type Caller, useCaller } from "../../shell/caller";
import { useRead, useWrite } from "../../shell/data";
import { formatAgo, formatDate } from "../../shell/format";
import { type Translate, useLanguage, useT } from "../../shell/i18n";
import { useShownOnce } from "../../shell/shownOnce";
import { ConfirmDialog } from "../../ui/ConfirmDialog";
import { Field } from "../../ui/fields";
import { useFocusFallback } from "../../ui/focus";
import { notify } from "../../ui/notify";
import { Loaded } from "../../ui/states";
import {
  defaultValidFor,
  IssueDialog,
  JoinCodeLine,
  type ValidFor,
  ValidForChoice,
  validForRequest,
} from "./IssueDialog";

/** DevicesProps are whose devices a list shows, and where. */
export interface DevicesProps {
  /** user names the User whose Device Sessions the list shows. */
  user: string;
  /** owner says user is the Owner, whose last live Device Session is revoked only once its name is typed. */
  owner: boolean;
  /** all shows the Revoked Device Sessions too, as device.list's all does. */
  all: boolean;
  onAll: (all: boolean) => void;
  /** level is the heading's level: 2 on a page, 3 in a panel. */
  level: 2 | 3;
  className?: string;
}

/**
 * Devices lists one User's Device Sessions (device.list), the Revoked ones
 * when all is set, the one the web UI runs through marked "This device";
 * adds one, its Join Code shown once (AddDevice); and revokes a live one
 * once the caller confirms (RevokeDevice). The Account page shows the
 * caller's own; a person's panel, the Owner's alone, shows that person's.
 */
export function Devices({ user, owner, all, onAll, level, className = "" }: DevicesProps) {
  const t = useT();
  const language = useLanguage();
  const caller = useCaller();
  const id = useId();
  const list = useRead(DeviceList, all ? { user, all: true } : { user });
  const [adding, setAdding] = useState(false);
  const [revoking, setRevoking] = useState<DeviceSession>();
  const Heading = level === 2 ? "h2" : "h3";
  const [heading, refocus] = useFocusFallback<HTMLHeadingElement>(list.data);
  const live = list.data?.device_sessions.filter((s) => s.revoked_at === undefined) ?? [];
  const ownersLast = (s: DeviceSession) => owner && live.length === 1 && live[0]?.id === s.id;
  return (
    <section aria-labelledby={id} className={`flex flex-col gap-3 ${className}`}>
      <div className="flex min-h-10 flex-wrap items-center justify-between gap-3">
        <Heading id={id} ref={heading} tabIndex={-1} className="t-heading flex items-center gap-2 outline-none">
          <Laptop aria-hidden className="size-5 text-muted" />
          {t("device.title")}
        </Heading>
        <Button variant="secondary" onPress={() => setAdding(true)}>
          <Plus aria-hidden className="size-4" />
          {t("device.add")}
        </Button>
      </div>
      <Loaded read={list}>
        {({ device_sessions: sessions }) =>
          sessions.length === 0 ? (
            <p className="text-muted">{t("device.none", { name: user })}</p>
          ) : (
            <ul className="flex flex-col">
              {sessions.map((s) => (
                <li key={s.id} className="flex items-center justify-between gap-4 border-t border-line py-3">
                  <div className="min-w-0">
                    <p className="flex flex-wrap items-center gap-2">
                      <span className={`t-name ${s.revoked_at === undefined ? "" : "text-muted line-through"}`}>
                        {s.device}
                      </span>
                      {s.id === caller.deviceSession?.id && (
                        <Chip color="accent" variant="soft">
                          {t("device.here")}
                        </Chip>
                      )}
                      {s.revoked_at !== undefined && <Chip variant="secondary">{t("device.revoked")}</Chip>}
                    </p>
                    <p className="t-label">
                      {s.revoked_at === undefined
                        ? `${t("device.joined", { date: formatDate(s.created_at) })} · ${
                            s.last_used_at === undefined
                              ? t("device.neverUsed")
                              : t("device.lastUsed", { ago: formatAgo(s.last_used_at, language) })
                          }`
                        : t("device.revokedOn", { date: formatDate(s.revoked_at) })}
                    </p>
                  </div>
                  {s.revoked_at === undefined && (
                    <Button
                      variant="danger-soft"
                      aria-label={t("device.revoke.label", { name: shownName(s, caller) })}
                      onPress={() => setRevoking(s)}
                    >
                      {t("device.revoke")}
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          )
        }
      </Loaded>
      <Switch isSelected={all} onChange={onAll} size="sm">
        <Switch.Content>
          <Switch.Control>
            <Switch.Thumb />
          </Switch.Control>
          {t("device.showRevoked")}
        </Switch.Content>
      </Switch>
      {adding && <AddDevice user={user} onAdded={list.reload} onClose={() => setAdding(false)} />}
      {revoking && (
        <RevokeDevice
          session={revoking}
          last={ownersLast(revoking)}
          onDone={() => {
            setRevoking(undefined);
            refocus();
            list.reload();
          }}
          onRefused={list.reload}
          onCancel={() => setRevoking(undefined)}
        />
      )}
    </section>
  );
}

/**
 * shownName names a Device Session as the CLI does: bare for one of the
 * caller's own devices, and <user>/<device> for another User's.
 */
function shownName(session: DeviceSession, caller: Caller): string {
  return session.user === caller.user.name ? session.device : `${session.user}/${session.device}`;
}

/**
 * revokeConsequence states what revoking session does: for the Owner's last
 * device, that only the koko server's own machine adds another; for the
 * device this page runs through, that the page stops and the device deletes
 * its key; for one that is both, both; else that the device is cut off.
 */
function revokeConsequence(t: Translate, session: DeviceSession, caller: Caller, last: boolean): string {
  const name = shownName(session, caller);
  const here = session.id === caller.deviceSession?.id;
  if (last) {
    return t(here ? "device.revoke.consequenceLastHere" : "device.revoke.consequenceLast", { name });
  }
  return t(here ? "device.revoke.consequenceHere" : "device.revoke.consequence", { name });
}

/**
 * RevokeDevice asks before it revokes a live Device Session, stating what
 * follows; the Owner's last live one (last) only once its name is typed,
 * and the call then carries last, as the CLI's --last does. A refusal shows
 * in the dialog and reloads the list (onRefused), so a list out of date,
 * such as one still showing a second device of the Owner's, is put right.
 */
function RevokeDevice({
  session,
  last,
  onDone,
  onRefused,
  onCancel,
}: {
  session: DeviceSession;
  last: boolean;
  onDone: () => void;
  onRefused: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const caller = useCaller();
  const revoke = useWrite(DeviceRevoke);
  const name = shownName(session, caller);
  return (
    <ConfirmDialog
      title={t("device.revoke.title", { name })}
      consequence={revokeConsequence(t, session, caller, last)}
      confirm={t("device.revoke.confirm")}
      typedName={last ? session.device : undefined}
      pending={revoke.pending}
      error={revoke.error}
      onCancel={onCancel}
      onConfirm={() =>
        void revoke.submit({ user: session.user, device: session.device, last: last || undefined }).then((revoked) => {
          if (revoked) {
            notify(t("device.revokedDone", { name }));
            onDone();
          } else {
            onRefused();
          }
        })
      }
    />
  );
}

/**
 * AddDevice issues a Join Code for a new device of user, with an optional
 * name, valid for the time chosen, and shows it once; it never asks first.
 * The caller's own device is asked for with no user, as device add does
 * without --user; another User's, the Owner's alone, names them.
 */
function AddDevice({ user, onAdded, onClose }: { user: string; onAdded: () => void; onClose: () => void }) {
  const t = useT();
  const caller = useCaller();
  const own = user === caller.user.name;
  const [device, setDevice] = useState("");
  const [validFor, setValidFor] = useState<ValidFor>(defaultValidFor);
  // The code shown once, with the validity it was issued for.
  const [issued, setIssued] = useShownOnce<{ joinCode: JoinCode; validFor: ValidFor }>();
  // done keeps the second step once the code is issued, so a code the browser forgot is not offered again.
  const [done, setDone] = useState(false);
  const add = useWrite(DeviceAdd);
  return (
    <IssueDialog
      title={own ? t("device.add") : t("device.add.titleFor", { name: user })}
      lead={t("device.add.lead")}
      submit={t("device.add.submit")}
      pending={add.pending}
      error={add.error}
      doneTitle={t("device.joinCode.title")}
      issued={
        !done ? undefined : issued ? (
          <JoinCodeLine
            joinCode={issued.joinCode}
            validFor={issued.validFor}
            expired={t("device.joinCode.expired")}
          />
        ) : (
          <p>{t("device.joinCode.forgotten")}</p>
        )
      }
      onSubmit={() =>
        void add
          .submit({ ...(device === "" ? {} : { device }), ...(own ? {} : { user }), ...validForRequest(validFor) })
          .then((added) => {
            if (added) {
              setIssued({ joinCode: added.join_code, validFor });
              setDone(true);
              onAdded();
            }
          })
      }
      onClose={() => {
        setIssued(undefined);
        onClose();
      }}
    >
      <Field
        label={t("device.add.name")}
        value={device}
        onChange={setDevice}
        description={t("device.add.nameHint")}
        code
        autoFocus
      />
      <ValidForChoice value={validFor} onChange={setValidFor} />
    </IssueDialog>
  );
}
