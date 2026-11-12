import { Button, buttonVariants, Link } from "@heroui/react";
import { ChevronRight, Pencil, Plus } from "lucide-react";
import { lazy, type ReactNode, type RefObject, Suspense, useRef, useState } from "react";
import { flushSync } from "react-dom";

import {
  CodeUnknownHost,
  type Host,
  HostMove,
  HostRemove,
  HostRename,
  HostRetire,
  HostShow,
} from "../../api/host";
import { SecretList } from "../../api/secret";
import type { Service } from "../../api/service";
import { type Space, SpaceList, type SpaceListResponse } from "../../api/space";
import { ApiError } from "../../client";
import { type Read, useRead, useWrite } from "../../shell/data";
import { formatAgo, formatDate } from "../../shell/format";
import { useCount, useLanguage, useT } from "../../shell/i18n";
import { navigate, type PageProps } from "../../shell/router";
import { ConfirmDialog } from "../../ui/ConfirmDialog";
import { CommandLine } from "../../ui/copy";
import { Field, SearchChoice } from "../../ui/fields";
import { FormDialog } from "../../ui/FormDialog";
import { notify } from "../../ui/notify";
import { Page, Sheet } from "../../ui/Page";
import { RowMenu } from "../../ui/RowMenu";
import { Loaded, LoadFailed } from "../../ui/states";
import { StateMark } from "../../ui/StateMark";
import { SecretRow } from "../secret/SecretRow";
import { Fact } from "./Fact";
import { keyLabel, purchase } from "./keys";
import { hostPath, hostPathOf, servicePath, validName } from "./record";

// Edit and a Service's panel bring the Host form's fields, and the calendar
// with them, so they load when first opened.
const EditHost = lazy(() => import("./HostForm").then((m) => ({ default: m.EditHost })));
const AddService = lazy(() => import("./ServicePanel").then((m) => ({ default: m.AddService })));
const OpenService = lazy(() => import("./ServicePanel").then((m) => ({ default: m.OpenService })));

/** Acting is the overlay one of the page's actions opened. */
type Acting = "edit" | "rename" | "move" | "retire" | "delete" | "addService";

/** secretsShown is how many of its Space's Secrets a Host's page shows. */
const secretsShown = 4;

/**
 * HostPage is /hosts/<name or id> (docs/web-design.md, "A Host"): what
 * host.show answers the caller, as `koko host show` shows it, with a
 * Service's panel over it at /hosts/<host>/services/<service>. A Host the
 * caller cannot see answers unknown_host, and shows as one that does not
 * exist.
 */
export function HostPage({ params }: PageProps) {
  const t = useT();
  const named = params["host"] ?? "";
  const show = useRead(HostShow, { host: named });
  if (show.data === undefined) {
    if (show.error instanceof ApiError && show.error.code === CodeUnknownHost) {
      return (
        <Page title={t("host.unknown.title")} lead={t("host.unknown.lead", { ref: named })}>
          <Link href="/hosts" className={buttonVariants({ variant: "secondary" })}>
            {t("host.unknown.back")}
          </Link>
        </Page>
      );
    }
    return (
      <Page title={named}>
        <Sheet list>
          <Loaded read={show}>{() => null}</Loaded>
        </Sheet>
      </Page>
    );
  }
  return (
    <HostView
      host={show.data.host}
      services={show.data.services}
      service={params["service"]}
      error={show.error}
      reload={show.reload}
    />
  );
}

/** HostViewProps are what a Host's page shows. */
interface HostViewProps {
  host: Host;
  services: readonly Service[];
  /** service is the name of the Service whose panel is open over the page, if one is. */
  service: string | undefined;
  /** error is why the last reload of host.show failed, if it did: what was shown stays under it. */
  error: unknown;
  reload: () => void;
}

/**
 * HostView is a Host's page: the breadcrumb, its name and state mark, Edit
 * and More actions; how to reach it and its Attributes; its Services, the
 * Secrets of its Space, its Note, and when it was recorded. Every action
 * names the Host by its id, so a rename made meanwhile never sends one to
 * another Host. A Retired Host offers Edit of its Attributes and Note, Move
 * and Delete with no pin; Rename and Retire are disabled, and its Services
 * show read-only.
 */
function HostView({ host, services, service, error, reload }: HostViewProps) {
  const t = useT();
  const language = useLanguage();
  const spaces = useRead(SpaceList, {});
  const [acting, setActing] = useState<Acting>();
  const addService = useRef<HTMLButtonElement>(null);
  const retired = host.state === "retired";
  const others = spaces.data?.spaces.filter((s) => s.name !== host.space);
  const close = () => setActing(undefined);
  const done = () => {
    close();
    reload();
  };
  const spaceList = `/hosts?${new URLSearchParams({ filter: `space=${host.space}` })}`;
  return (
    <Page
      name={host.name}
      breadcrumb={
        <nav aria-label={t("host.page.breadcrumb")} className="flex items-center gap-2 text-sm">
          <Link href="/hosts" className="text-muted no-underline hover:text-foreground">
            {t("host.list.title")}
          </Link>
          <ChevronRight aria-hidden className="size-3.5 text-muted" />
          <Link href={spaceList} className="text-muted no-underline hover:text-foreground">
            {host.space}
          </Link>
        </nav>
      }
      title={
        <span className="flex flex-wrap items-center gap-x-4 gap-y-1">
          <span className="min-w-0 break-all">{host.name}</span>
          <span className="tracking-normal">
            <StateMark state={host.state} />
          </span>
        </span>
      }
      actions={
        <>
          <Button variant="secondary" onPress={() => setActing("edit")}>
            <Pencil aria-hidden className="size-4" />
            {t("host.edit.open")}
          </Button>
          <RowMenu
            label={t("host.page.more", { name: host.name })}
            variant="secondary"
            actions={[
              { id: "rename", label: t("host.rename"), onAction: () => setActing("rename"), isDisabled: retired },
              { id: "move", label: t("host.move"), onAction: () => setActing("move"), isDisabled: others?.length === 0 },
              {
                id: "retire",
                label: t("host.retire"),
                onAction: () => setActing("retire"),
                danger: true,
                isDisabled: retired,
              },
              // Delete is offered only to a Host with no pin: one that has been Managed is kept on record.
              ...(host.host_key === null
                ? [{ id: "delete", label: t("host.remove"), onAction: () => setActing("delete"), danger: true }]
                : []),
            ]}
          />
        </>
      }
    >
      {error !== undefined && (
        <div className="mb-5">
          <Sheet list>
            <LoadFailed error={error} retry={reload} />
          </Sheet>
        </div>
      )}
      {/* Two columns once the page's own room allows, beside the rail as on a narrower window. */}
      <div className="@container">
        <div className="grid items-start gap-5 @4xl:grid-cols-3">
          <div className="flex min-w-0 flex-col gap-5 @4xl:col-span-2">
            <Reaching host={host} />
            <Attributes host={host} />
          </div>
          <div className="flex min-w-0 flex-col gap-5">
            <Services
              services={services}
              onAdd={retired ? undefined : () => setActing("addService")}
              addRef={addService}
            />
            <SecretsIn host={host} spaces={spaces.data?.spaces} />
            <Sheet title={t("host.key.note")}>
              {host.note === undefined ? (
                <p className="text-sm text-muted">{t("host.page.noNote")}</p>
              ) : (
                <p className="text-sm break-words whitespace-pre-wrap text-ink-2">{host.note}</p>
              )}
            </Sheet>
            <p className="t-label px-1">
              {t("host.page.recorded", {
                created: formatDate(host.created_at),
                changed: formatAgo(host.updated_at, language),
              })}{" "}
              · <span className="t-code break-all">{host.id}</span>
            </p>
          </div>
        </div>
      </div>

      <Suspense>
        {acting === "edit" && <EditHost host={host} onSaved={done} onClose={close} />}
        {acting === "addService" && (
          <AddService
            host={host}
            onClose={close}
            onAdded={(added) => {
              // The new Service's panel takes the place of Add Service's, and gives the focus back to
              // Add Service in turn: the focus is on it as that panel opens.
              flushSync(close);
              addService.current?.focus();
              reload();
              navigate(servicePath(added.host, added.name));
            }}
          />
        )}
        {service !== undefined && (
          <OpenService
            key={service}
            host={host}
            name={service}
            onChanged={reload}
            onClose={() => navigate(hostPath(host), { replace: true })}
          />
        )}
      </Suspense>
      {acting === "rename" && <RenameHost host={host} onClose={close} />}
      {acting === "move" && <MoveHost host={host} spaces={spaces} onDone={done} onClose={close} />}
      {acting === "retire" && <RetireHost host={host} onClose={close} />}
      {acting === "delete" && <DeleteHost host={host} services={services.length} onClose={close} />}
    </Page>
  );
}

/**
 * Reaching is how to reach the Host, by its state: Managed, the ssh line;
 * Registered, the line that enrolls it; Retired, that it can no longer be
 * reached. Under them, its address, port, Host Account, Relay and pin.
 */
function Reaching({ host }: { host: Host }) {
  const t = useT();
  return (
    <Sheet title={t("host.page.reach")}>
      {host.state === "managed" && (
        <>
          <CommandLine command={`ssh ${host.name}`} label={t("host.page.sshCommand")} />
          <p className="mt-3 max-w-[66ch] text-sm text-muted">
            {t("host.page.reachManaged", { account: host.account })}
          </p>
        </>
      )}
      {host.state === "registered" && (
        <>
          <p className="mb-3 max-w-[66ch] text-sm text-ink-2">{t("host.page.reachRegistered")}</p>
          <CommandLine command={`koko host enroll ${host.name}`} label={t("host.page.enrollCommand")} />
        </>
      )}
      {host.state === "retired" && <p className="max-w-[66ch] text-sm text-muted">{t("host.page.reachRetired")}</p>}
      <dl className="mt-5 grid grid-cols-2 gap-x-8 gap-y-5 border-t border-line pt-5 sm:grid-cols-3">
        <Fact label={t("host.key.address")} className="col-span-2 sm:col-span-1">
          <span className="t-code">{host.address}</span>
        </Fact>
        <Fact label={t("host.key.port")}>
          <span className="t-code">{host.port}</span>
        </Fact>
        <Fact label={t("host.key.account")}>
          <span className="t-code">{host.account}</span>
        </Fact>
        {host.relay !== null && (
          <Fact label={t("host.key.relay")}>
            {host.relay.id === null || host.relay.name === null ? (
              <span className="text-muted">{t("host.relay.unseen")}</span>
            ) : (
              <Link href={hostPathOf(host.relay.id)}>{host.relay.name}</Link>
            )}
          </Fact>
        )}
        <Fact label={t("host.key.host_key")} className="col-span-2 sm:col-span-3">
          {host.host_key === null ? (
            <span className="text-muted">{t("host.page.notPinned")}</span>
          ) : (
            <span className="t-code break-all">{host.host_key}</span>
          )}
        </Fact>
      </dl>
    </Sheet>
  );
}

/** purchaseOrder are the Preset keys the Attributes show first, in the purchase's order, the currency with the price. */
const purchaseOrder: readonly string[] = purchase.filter((k) => k !== "currency");

/**
 * Attributes are the Host's Attributes as stored, a label over each value,
 * four across: the purchase first, the price with its currency, then every
 * other key. An expiry is a plain Attribute.
 */
function Attributes({ host }: { host: Host }) {
  const t = useT();
  const a = host.attributes;
  const keys = [
    ...purchaseOrder.filter((k) => Object.hasOwn(a, k)),
    // A currency shows after its price, or alone with none.
    ...(Object.hasOwn(a, "currency") && !Object.hasOwn(a, "price") ? ["currency"] : []),
    ...Object.keys(a)
      .filter((k) => !purchaseOrder.includes(k) && k !== "currency")
      .sort(),
  ];
  const value = (key: string): ReactNode => {
    switch (key) {
      case "price":
        return <span className="t-num">{[a.price, a.currency].filter((v) => v !== undefined).join(" ")}</span>;
      case "expires_at":
        return <span className="t-num">{a.expires_at}</span>;
      default:
        return a[key];
    }
  };
  return (
    <Sheet title={t("host.page.attributes")}>
      {keys.length === 0 ? (
        <p className="text-sm text-muted">{t("host.page.noAttributes")}</p>
      ) : (
        <dl className="grid grid-cols-2 gap-x-8 gap-y-5 sm:grid-cols-4">
          {keys.map((key) => (
            <Fact key={key} label={keyLabel(t, key)}>
              {value(key)}
            </Fact>
          ))}
        </dl>
      )}
    </Sheet>
  );
}

/**
 * Services are the Host's Services, each a row with its name and ports
 * that opens its panel, and Add Service, unless the Host is Retired, whose
 * Services are read-only.
 */
function Services({
  services,
  onAdd,
  addRef,
}: {
  services: readonly Service[];
  onAdd: (() => void) | undefined;
  addRef: RefObject<HTMLButtonElement | null>;
}) {
  const t = useT();
  return (
    <Sheet
      title={t("host.services.title")}
      action={
        onAdd && (
          <Button ref={addRef} size="sm" variant="tertiary" onPress={onAdd}>
            <Plus aria-hidden className="size-4" />
            {t("service.add")}
          </Button>
        )
      }
    >
      {services.length === 0 ? (
        <p className="text-sm text-muted">{t("host.page.noServices")}</p>
      ) : (
        <ul aria-label={t("host.services.title")}>
          {services.map((s) => (
            <li key={s.id} className="border-t border-line">
              <Link
                href={servicePath(s.host, s.name)}
                className="row-link -mx-2 flex min-h-11 w-[calc(100%+1rem)] items-center justify-between gap-4 px-2 text-foreground no-underline"
              >
                <span className="font-medium">{s.name}</span>
                <span className="t-code text-right text-ink-2">
                  {s.ports.length === 0 ? (
                    <span className="font-sans text-muted">{t("host.services.noPorts")}</span>
                  ) : (
                    s.ports.join("  ")
                  )}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </Sheet>
  );
}

/**
 * SecretsIn are up to four of the Secrets of the Host's Space, those whose
 * name holds the Host's name first, each with Copy, Show and its menu as on
 * the Secrets page, and All N, a link to the Secrets page narrowed to the
 * Space, when there are more.
 */
function SecretsIn({ host, spaces }: { host: Host; spaces: readonly Space[] | undefined }) {
  const t = useT();
  const read = useRead(SecretList, { space: host.space });
  const title = t("host.page.secretsIn", { space: host.space });
  const secrets = read.data?.secrets;
  const named = (s: { name: string }) => s.name.includes(host.name);
  const shown = secrets && [...secrets.filter(named), ...secrets.filter((s) => !named(s))].slice(0, secretsShown);
  return (
    <Sheet
      title={title}
      action={
        secrets !== undefined &&
        shown !== undefined &&
        secrets.length > shown.length && (
          <Link href={`/secrets?${new URLSearchParams({ space: host.space })}`} className="text-sm">
            {t("host.page.allSecrets", { count: secrets.length })}
          </Link>
        )
      }
    >
      <Loaded read={read}>
        {() =>
          shown === undefined || shown.length === 0 ? (
            <p className="text-sm text-muted">{t("host.page.noSecrets", { space: host.space })}</p>
          ) : (
            <ul aria-label={title}>
              {shown.map((s) => (
                <SecretRow key={s.id} secret={s} spaces={spaces} onChanged={read.reload} compact />
              ))}
            </ul>
          )
        }
      </Loaded>
    </Sheet>
  );
}

/** RenameHost gives the Host a new name, its SSH Name; the page then moves to it, replacing its history entry. */
function RenameHost({ host, onClose }: { host: Host; onClose: () => void }) {
  const t = useT();
  const [name, setName] = useState(host.name);
  const rename = useWrite(HostRename);
  return (
    <FormDialog
      title={t("host.rename.title", { name: host.name })}
      lead={t("host.rename.lead")}
      submit={t("host.rename")}
      pending={rename.pending}
      changed={name !== host.name}
      error={rename.error}
      onSubmit={() =>
        void rename.submit({ host: host.id, new_name: name.trim() }).then((renamed) => {
          if (renamed) {
            notify(t("host.rename.done", { name: renamed.host.name }));
            onClose();
            // The old name names no Host now, so it leaves no history entry.
            navigate(hostPath(renamed.host), { replace: true });
          }
        })
      }
      onClose={onClose}
    >
      <Field
        label={t("host.rename.newName")}
        value={name}
        onChange={setName}
        validate={(v) => (validName(v.trim()) ? undefined : t("host.form.nameRule"))}
        code
        autoFocus
      />
    </FormDialog>
  );
}

/** MoveHost moves the Host, under its name, to another Space the caller can act in, picked in a searchable choice. */
function MoveHost({
  host,
  spaces,
  onDone,
  onClose,
}: {
  host: Host;
  /** spaces is space.list's read, whose failure the dialog shows in place of an empty choice. */
  spaces: Read<SpaceListResponse>;
  onDone: () => void;
  onClose: () => void;
}) {
  const t = useT();
  const [space, setSpace] = useState<string | null>(null);
  const move = useWrite(HostMove);
  const others = spaces.data?.spaces.filter((s) => s.name !== host.space) ?? [];
  return (
    <FormDialog
      title={t("host.move.title", { name: host.name })}
      lead={t("host.move.lead", { space: host.space })}
      submit={t("host.move.submit")}
      pending={move.pending}
      changed={space !== null}
      error={move.error ?? spaces.error}
      onSubmit={() =>
        void move.submit({ host: host.id, space: space ?? "" }).then((moved) => {
          if (moved) {
            notify(t("host.move.done", { name: host.name, space: space ?? "" }));
            onDone();
          }
        })
      }
      onClose={onClose}
    >
      <SearchChoice
        label={t("host.move.to")}
        value={space}
        onChange={setSpace}
        options={others.map((s) => ({
          id: s.name,
          label: s.name,
          description: s.personal ? t("host.list.personal") : undefined,
        }))}
        validate={(v) => (v === null ? t("host.move.required") : undefined)}
      />
    </FormDialog>
  );
}

/**
 * RetireHost asks first, saying what follows, then retires the Host; the
 * page moves to /hosts/<id>, replacing its history entry, since its name
 * no longer names it. relay_in_use shows in place.
 */
function RetireHost({ host, onClose }: { host: Host; onClose: () => void }) {
  const t = useT();
  const retire = useWrite(HostRetire);
  return (
    <ConfirmDialog
      title={t("host.retire.title", { name: host.name })}
      consequence={t("host.retire.consequence", { name: host.name, id: host.id })}
      confirm={t("host.retire")}
      pending={retire.pending}
      error={retire.error}
      onConfirm={() =>
        void retire.submit({ host: host.id }).then((retired) => {
          if (retired) {
            notify(t("host.retire.done", { name: host.name }));
            onClose();
            // A Retired Host is named by its id alone.
            navigate(hostPathOf(host.id), { replace: true });
          }
        })
      }
      onCancel={onClose}
    />
  );
}

/**
 * DeleteHost asks first, saying what goes with the Host, its Services
 * counted, then deletes it; the page goes back to /hosts, replacing its
 * history entry. host_was_managed shows in place.
 */
function DeleteHost({ host, services, onClose }: { host: Host; services: number; onClose: () => void }) {
  const t = useT();
  const count = useCount();
  const remove = useWrite(HostRemove);
  return (
    <ConfirmDialog
      title={t("host.remove.title", { name: host.name })}
      consequence={
        services === 0
          ? t("host.remove.consequenceAlone", { name: host.name })
          : t("host.remove.consequence", { name: host.name, services: count("host.count.services", services) })
      }
      confirm={t("host.remove")}
      pending={remove.pending}
      error={remove.error}
      onConfirm={() =>
        void remove.submit({ host: host.id }).then((removed) => {
          if (removed) {
            notify(t("host.remove.done", { name: host.name }));
            onClose();
            navigate("/hosts", { replace: true });
          }
        })
      }
      onCancel={onClose}
    />
  );
}
