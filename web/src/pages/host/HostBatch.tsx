import { Archive, FolderInput, Tag, Trash2 } from "lucide-react";
import { lazy, type ReactNode, Suspense, useState } from "react";

import type { Op } from "../../api/api";
import type { AttrKey } from "../../api/attr";
import { type Host, HostMove, HostRemove, HostRetire, HostSet, type HostSetRequest } from "../../api/host";
import type { Space } from "../../api/space";
import { useWrite } from "../../shell/data";
import { useCount, useT } from "../../shell/i18n";
import { Field, SearchChoice, Segments } from "../../ui/fields";
import { BatchDialog, type BatchWrite, type Item } from "../batch/BatchDialog";
import { type SelectMode, useSelectMode } from "../batch/useSelectMode";
import { wordsOf } from "../batch/words";
import { typeOf } from "./AttributeRows";
import { EnumChoice } from "./EnumChoice";
import { type Clock, readValue, validAttrKey } from "./view";

// The Hosts list's Select mode (#134): the actions its bar offers, and the
// one dialog each asks in. Each action is one call of its operation's
// batch form, applied to every Host or to none (ADR 0050).

// The date field brings the calendar with it, so it loads when a date's value is first asked for.
const DateValue = lazy(() => import("./DateValue").then((m) => ({ default: m.DateValue })));

/** HostAction is an action a batch of Hosts takes. */
type HostAction = "move" | "attribute" | "retire" | "delete";

/** hostItem is a Host as a batch's dialog lists it: its name, then its Space. */
function hostItem(host: Host): Item<Host> {
  return { id: host.id, name: host.name, detail: host.space, record: host };
}

/**
 * useHostsWrite is the batch form of a Host write: op for the Hosts given,
 * by id, with what the rest of its request holds.
 */
function useHostsWrite<Req extends { hosts?: string[] }>(
  op: Op<Req, unknown>,
  rest: () => Omit<Req, "host" | "hosts">,
): BatchWrite<Host> {
  const write = useWrite(op);
  return {
    send: async (hosts) => (await write.submit({ ...rest(), hosts: hosts.map((h) => h.id) } as Req)) !== undefined,
    pending: write.pending,
    error: write.error,
  };
}

/**
 * useHostSelectMode holds the Hosts list's Select mode (useSelectMode) and
 * its actions: Move to a Space, set or remove an Attribute, Retire and
 * Delete.
 */
export function useHostSelectMode({
  recorded,
  spaces,
  attrs,
  clock,
}: {
  /** recorded are every Host the caller can see, Retired ones too. */
  recorded: readonly Host[];
  spaces: readonly Space[];
  attrs: readonly AttrKey[];
  clock: Clock;
}): { mode: SelectMode; select: ReactNode; overlays: ReactNode } {
  const t = useT();
  return useSelectMode<Host, HostAction>({
    records: recorded,
    idOf: (h) => h.id,
    barLabel: t("batch.host.bar"),
    actions: [
      { id: "move", label: t("batch.action.move"), icon: FolderInput },
      { id: "attribute", label: t("batch.action.attribute"), icon: Tag },
      { id: "retire", label: t("host.retire"), icon: Archive, danger: true },
      { id: "delete", label: t("host.remove"), icon: Trash2, danger: true },
    ],
    dialog: (action, hosts, onClose) => {
      switch (action) {
        case "move":
          return <MoveHosts hosts={hosts} spaces={spaces} onClose={onClose} />;
        case "attribute":
          return <AttributeOfHosts hosts={hosts} attrs={attrs} clock={clock} onClose={onClose} />;
        case "retire":
          return <RetireHosts hosts={hosts} onClose={onClose} />;
        case "delete":
          return <DeleteHosts hosts={hosts} onClose={onClose} />;
      }
    },
  });
}

/** DialogProps are what an action's dialog gets. */
interface DialogProps {
  /** hosts are the Hosts selected. */
  hosts: readonly Host[];
  onClose: (applied: boolean) => void;
}

/** useThings counts Hosts in words: "1 Host", "20 Hosts". */
function useThings(): (n: number) => string {
  const count = useCount();
  return (n) => count("host.count.hosts", n);
}

/** MoveHosts moves Hosts to one Space; those in it already are left out. */
function MoveHosts({ hosts, spaces, onClose }: DialogProps & { spaces: readonly Space[] }) {
  const t = useT();
  const count = useCount();
  const things = useThings();
  const [space, setSpace] = useState<string | null>(null);
  const moving = space === null ? hosts : hosts.filter((h) => h.space !== space);
  const staying = hosts.length - moving.length;
  const to = space ?? "";
  const write = useHostsWrite(HostMove, () => ({ space: to }));
  const title = (n: number) => t("batch.host.move.title", { things: things(n) });
  return (
    <BatchDialog
      title={title}
      lead={t("batch.host.move.lead")}
      kind={t("host.list.title")}
      items={moving.map(hostItem)}
      leftOut={staying > 0 ? count("batch.host.move.staying", staying, { space: to }) : undefined}
      submit={title}
      write={write}
      words={wordsOf(t, count, "batch.host.move", { space: to })}
      changed={space !== null}
      onClose={onClose}
    >
      <SearchChoice
        label={t("host.move.to")}
        value={space}
        onChange={setSpace}
        // A Space that holds every one of them already is no move.
        options={spaces
          .filter((s) => hosts.some((h) => h.space !== s.name))
          .map((s) => ({ id: s.name, label: s.name, description: s.personal ? t("host.list.personal") : undefined }))}
        validate={(v) => (v === null ? t("host.move.required") : undefined)}
      />
    </BatchDialog>
  );
}

/**
 * AttributeOfHosts sets one Attribute on Hosts, or removes one from those
 * that have it; a Host without the key being removed is left out. A
 * Retired Host takes the change as any other.
 */
function AttributeOfHosts({ hosts, attrs, clock, onClose }: DialogProps & { attrs: readonly AttrKey[]; clock: Clock }) {
  const t = useT();
  const count = useCount();
  const things = useThings();
  const [mode, setMode] = useState("set");
  const [key, setKey] = useState<string | null>(null);
  const [value, setValue] = useState("");
  const removing = mode === "remove";
  const name = key ?? "";
  const type = typeOf(attrs, name);
  const changing = removing && key !== null ? hosts.filter((h) => h.attributes[key] !== undefined) : hosts;
  const without = hosts.length - changing.length;
  const inUse = [...new Set(hosts.flatMap((h) => Object.keys(h.attributes)))].sort();
  const keys = removing ? inUse : [...new Set([...attrs.map((a) => a.key), ...inUse])];

  const write = useHostsWrite(HostSet, (): Omit<HostSetRequest, "host" | "hosts"> => {
    if (removing) {
      return { unset: [name] };
    }
    const read = readValue(type, value.trim(), clock);
    return { attributes: read === undefined ? {} : { [name]: read } };
  });
  const validateValue = (v: string) =>
    v.trim() === ""
      ? t("host.form.required")
      : readValue(type, v.trim(), clock) === undefined
        ? type.type === "date"
          ? t("host.date.invalid", { value: v })
          : t("host.form.invalid", { key: name })
        : undefined;
  const valueLabel = key === null ? t("host.filter.value") : t("host.form.attrValue", { key });

  return (
    <BatchDialog
      title={(n) =>
        key === null
          ? t("batch.host.attribute.title", { things: things(n) })
          : t(removing ? "batch.host.unset.title" : "batch.host.set.title", { things: things(n), key })
      }
      lead={t(removing ? "batch.host.unset.lead" : "batch.host.set.lead")}
      kind={t("host.list.title")}
      items={changing.map(hostItem)}
      leftOut={(targets) => {
        if (key === null) {
          return undefined;
        }
        if (removing) {
          return without > 0 ? count("batch.host.unset.without", without, { key }) : undefined;
        }
        const replaced = targets.filter((item) => item.record.attributes[key] !== undefined).length;
        return replaced > 0 ? count("batch.host.set.holding", replaced, { key }) : undefined;
      }}
      submit={(n) => t(removing ? "batch.host.unset.submit" : "batch.host.set.submit", { things: things(n) })}
      write={write}
      words={wordsOf(t, count, removing ? "batch.host.unset" : "batch.host.set", { key: name })}
      changed={key !== null || value !== ""}
      onClose={onClose}
    >
      <Segments
        label={t("batch.host.attribute.mode")}
        value={mode}
        onChange={(next) => {
          setMode(next);
          setKey(null);
          setValue("");
        }}
        options={[
          { id: "set", label: t("batch.host.attribute.set") },
          { id: "remove", label: t("batch.host.attribute.remove") },
        ]}
      />
      <div className={`grid items-start gap-3 ${removing ? "" : "sm:grid-cols-2"}`}>
        <SearchChoice
          // The list differs with the mode, and so does whether a new key may be typed.
          key={mode}
          label={t("host.form.attrKey")}
          value={key}
          onChange={(next) => {
            setKey(next);
            setValue("");
          }}
          options={keys.map((k) => ({ id: k, label: k }))}
          newValues={!removing}
          validate={(k) =>
            k === null || k === ""
              ? t("host.form.required")
              : !removing && !validAttrKey(k)
                ? t("host.form.keyRule")
                : undefined
          }
        />
        {!removing &&
          (type.type === "enum" ? (
            <EnumChoice label={valueLabel} type={type} value={value} onChange={setValue} validate={validateValue} />
          ) : type.type === "date" ? (
            <Suspense>
              <DateValue label={valueLabel} value={value} onChange={setValue} validate={validateValue} />
            </Suspense>
          ) : (
            <Field
              label={valueLabel}
              value={value}
              onChange={setValue}
              validate={validateValue}
              inputMode={type.type === "number" ? "decimal" : undefined}
            />
          ))}
      </div>
    </BatchDialog>
  );
}

/**
 * RetireHosts asks once for them all, saying what follows. Hosts Retired
 * already are left out. A Relay and the Hosts behind it retire in one
 * batch, since the koko server counts only the Hosts outside it as using
 * the Relay.
 */
function RetireHosts({ hosts, onClose }: DialogProps) {
  const t = useT();
  const count = useCount();
  const things = useThings();
  const retiring = hosts.filter((h) => h.state !== "retired");
  const retired = hosts.length - retiring.length;
  const write = useHostsWrite(HostRetire, () => ({}));
  const title = (n: number) => t("batch.host.retire.title", { things: things(n) });
  return (
    <BatchDialog
      danger
      title={title}
      lead={t(retiring.length === 0 ? "batch.host.retire.nothing" : "batch.host.retire.lead")}
      kind={t("host.list.title")}
      items={retiring.map(hostItem)}
      leftOut={retired > 0 && retiring.length > 0 ? count("batch.host.retire.retired", retired) : undefined}
      submit={title}
      write={write}
      words={wordsOf(t, count, "batch.host.retire")}
      onClose={onClose}
    />
  );
}

/**
 * DeleteHosts asks once for them all, and, for more than one, for their
 * number typed. A Host that has been Managed, as its pinned host key
 * shows, is never deleted, so it is left out before anything is sent.
 */
function DeleteHosts({ hosts, onClose }: DialogProps) {
  const t = useT();
  const count = useCount();
  const things = useThings();
  const deleting = hosts.filter((h) => h.host_key === null);
  const kept = hosts.length - deleting.length;
  const write = useHostsWrite(HostRemove, () => ({}));
  const title = (n: number) => t("batch.host.remove.title", { things: things(n) });
  return (
    <BatchDialog
      danger
      title={title}
      lead={deleting.length === 0 ? count("batch.host.remove.kept", kept) : t("batch.host.remove.lead")}
      kind={t("host.list.title")}
      items={deleting.map(hostItem)}
      leftOut={kept > 0 && deleting.length > 0 ? count("batch.host.remove.kept", kept) : undefined}
      typed={deleting.length > 1}
      submit={title}
      write={write}
      words={wordsOf(t, count, "batch.host.remove")}
      onClose={onClose}
    />
  );
}
