import { AlertDialog, Button, Input, Label, Modal, TextField } from "@heroui/react";
import { type ReactNode, useId, useState } from "react";

import { ApiError } from "../../client";
import { reloadReads } from "../../shell/data";
import { errorMessage, useT } from "../../shell/i18n";
import { Form } from "../../ui/Form";
import { notify } from "../../ui/notify";
import { Refusal } from "../../ui/states";

// The one dialog of a batch (#134): what it does, to how many and which
// records, the form or the confirmation, and, when the koko server refused
// the batch, which records are at fault and why. A batch is one call,
// applied in full or not at all (ADR 0050).

/** Item is one record of a batch, as its dialog lists it. */
export interface Item<R> {
  id: string;
  /** name is what the dialog lists the record as: a Host's name, or <space>/<secret>. */
  name: string;
  /** detail is said after the name, muted: a Host's Space. */
  detail?: string;
  record: R;
}

/** BatchWrite is a batch's write, as a dialog's useWrite holds it. */
export interface BatchWrite<R> {
  /** send sends the batch form for records in one call, and answers whether it was applied. */
  send: (records: readonly R[]) => Promise<boolean>;
  pending: boolean;
  /** error is why the last call failed, if it did. */
  error?: unknown;
}

/** Words are what a batch's dialog says of how it ended. */
export interface Words {
  /** done is the notice once the batch was applied, such as "20 Hosts are now in ops." */
  done: (count: number) => string;
  /** none heads a batch the koko server refused, such as "No Host was moved". */
  none: string;
  /** rest labels the button that sends the batch again without the records at fault, such as "Move the other 17 Hosts". */
  rest: (count: number) => string;
}

/** BatchDialogProps are what a batch's dialog shows and does. */
export interface BatchDialogProps<R> {
  /** danger makes it a confirmation of what cannot be undone, as Retire and Delete are. */
  danger?: boolean;
  /** title names the action and how many records it acts on, such as "Move 20 Hosts". */
  title: (count: number) => string;
  /** lead says what the action does, and for a confirmation what is lost. */
  lead: ReactNode;
  /** kind names the records over their list, such as "Hosts". */
  kind: string;
  /** items are the records the action acts on: those selected, without the ones it leaves out. */
  items: readonly Item<R>[];
  /** leftOut says which of the records selected the action leaves out, and why, or what else is true of those it acts on. */
  leftOut?: ReactNode | ((targets: readonly Item<R>[]) => ReactNode);
  /** typed asks for the number of records to be typed before the action can run. */
  typed?: boolean;
  /** submit labels the button that acts, such as "Move 20 Hosts". */
  submit: (count: number) => string;
  write: BatchWrite<R>;
  words: Words;
  /** changed says a field holds something typed or chosen: a click beside the dialog then no longer closes it. */
  changed?: boolean;
  /** onClose hears whether the batch was applied. */
  onClose: (applied: boolean) => void;
  /** children are the form's fields. */
  children?: ReactNode;
}

/** Fault is a record a batch was refused for, and why. */
interface Fault<R> {
  item: Item<R>;
  error: ApiError;
}

/**
 * faultsOf are the records at fault in the refusal of the batch sent, each
 * with the error it would be refused with alone, or none when the call
 * failed otherwise, as a dropped connection does.
 */
function faultsOf<R>(error: unknown, sent: readonly Item<R>[]): Fault<R>[] {
  if (!(error instanceof ApiError) || error.items === undefined) {
    return [];
  }
  return error.items.flatMap((fault) => {
    const item = sent[fault.index];
    return item ? [{ item, error: new ApiError(fault.code, fault.message) }] : [];
  });
}

/**
 * BatchDialog asks once for a whole batch: it names the action, how many
 * records it acts on and which, and what follows. Applied, it says so in a
 * notice, loads the page's reads again and closes. A batch the koko server
 * refused changed nothing: the dialog stays, names each record at fault
 * with its reason, and offers to send the batch again without them. Any
 * other failure shows in place, the form kept.
 */
export function BatchDialog<R>(props: BatchDialogProps<R>) {
  const { danger = false, items, typed = false, write, words, onClose } = props;
  const t = useT();
  const leadId = useId();
  // The records at fault a batch sent again goes without.
  const [left, setLeft] = useState<ReadonlySet<string>>(new Set());
  const [sent, setSent] = useState<readonly Item<R>[]>([]);
  const [typedCount, setTypedCount] = useState("");

  const targets = items.filter((item) => !left.has(item.id));
  const faults = faultsOf(write.error, sent);
  const refused = faults.length > 0;
  const ready = targets.length > 0 && (!typed || typedCount.trim() === String(targets.length));

  const send = async (batch: readonly Item<R>[]) => {
    setSent(batch);
    if (await write.send(batch.map((item) => item.record))) {
      notify(words.done(batch.length));
      reloadReads();
      onClose(true);
    }
  };
  const cancel = () => onClose(false);

  const note = typeof props.leftOut === "function" ? props.leftOut(targets) : props.leftOut;
  const D = danger ? AlertDialog : Modal;
  const heading = refused ? words.none : props.title(targets.length);
  let body: ReactNode;
  let footer: ReactNode;
  if (refused) {
    const rest = sent.filter((item) => !faults.some((f) => f.item.id === item.id));
    body = (
      <div role="alert" className="flex flex-col gap-3">
        <p className="text-muted">{t("batch.refused.why", { count: faults.length, total: sent.length })}</p>
        <ul
          aria-label={t("batch.refused.atFault")}
          // A long list scrolls in place, and a keyboard reaches what scrolls.
          tabIndex={0}
          className="flex max-h-56 flex-col overflow-y-auto rounded-xl border border-border bg-background outline-none focus-visible:ring-2 focus-visible:ring-focus"
        >
          {faults.map(({ item, error }) => (
            <li key={item.id} className="border-b border-line px-3.5 py-2 last:border-0">
              <span className="font-medium">{item.name}</span>
              <span className="block text-sm text-muted">{errorMessage(t, error)}</span>
            </li>
          ))}
        </ul>
      </div>
    );
    footer = (
      <>
        <Button variant="tertiary" autoFocus={rest.length === 0} onPress={cancel}>
          {t(rest.length === 0 ? "shell.close" : "shell.cancel")}
        </Button>
        {rest.length > 0 && (
          <Button
            variant={danger ? "danger" : "primary"}
            autoFocus
            onPress={() => {
              setLeft((now) => new Set([...now, ...faults.map((f) => f.item.id)]));
              void send(rest);
            }}
          >
            {words.rest(rest.length)}
          </Button>
        )}
      </>
    );
  } else {
    body = (
      <>
        <p id={leadId} className="text-muted">
          {props.lead}
        </p>
        {props.children}
        {targets.length > 0 && <Targets kind={props.kind} items={targets} />}
        {note !== undefined && note !== null && note !== false && <p className="text-sm text-muted">{note}</p>}
        {typed && targets.length > 0 && (
          <TextField value={typedCount} onChange={setTypedCount} isDisabled={write.pending} autoFocus>
            <Label>{t("batch.typeCount", { count: targets.length })}</Label>
            <Input className="t-code" inputMode="numeric" autoComplete="off" />
          </TextField>
        )}
        <Refusal error={write.error} />
      </>
    );
    footer = (
      <>
        <Button variant="tertiary" autoFocus={danger && !typed} isDisabled={write.pending} onPress={cancel}>
          {t(targets.length === 0 ? "shell.close" : "shell.cancel")}
        </Button>
        {targets.length > 0 && (
          <Button
            type={danger ? "button" : "submit"}
            variant={danger ? "danger" : "primary"}
            isPending={write.pending}
            isDisabled={!ready}
            onPress={danger ? () => void send(targets) : undefined}
          >
            {props.submit(targets.length)}
          </Button>
        )}
      </>
    );
  }

  const content = (
    <>
      <D.Header>
        {danger && <AlertDialog.Icon status="danger" />}
        <D.Heading className={danger ? undefined : "t-title"}>{heading}</D.Heading>
      </D.Header>
      <D.Body className={`flex flex-col gap-4 ${danger ? "" : "p-1"}`}>{body}</D.Body>
      <D.Footer>{footer}</D.Footer>
    </>
  );

  return (
    <D.Backdrop
      isOpen
      // A click beside it closes it unless that would lose what was typed or chosen, or its call runs.
      isDismissable={!write.pending && (refused || (props.changed !== true && typedCount === ""))}
      isKeyboardDismissDisabled={write.pending}
      onOpenChange={(open) => open || write.pending || cancel()}
    >
      <D.Container size={danger ? "sm" : "md"}>
        <D.Dialog aria-label={danger ? undefined : heading} aria-describedby={refused ? undefined : leadId}>
          {danger ? (
            content
          ) : (
            <Form label={heading} onSubmit={() => void send(targets)}>
              <Modal.CloseTrigger aria-label={t("shell.close")} />
              {content}
            </Form>
          )}
        </D.Dialog>
      </D.Container>
    </D.Backdrop>
  );
}

/** Targets names the records a batch acts on, under their kind and number, in a list that scrolls in place. */
function Targets<R>({ kind, items }: { kind: string; items: readonly Item<R>[] }) {
  const id = useId();
  return (
    <div className="overflow-hidden rounded-xl border border-border bg-background">
      <p id={id} className="t-label border-b border-line px-3.5 py-2">
        {kind} <span className="t-num">{items.length}</span>
      </p>
      <ul
        aria-labelledby={id}
        // A long list scrolls in place, and a keyboard reaches what scrolls.
        tabIndex={0}
        className="grid max-h-44 gap-x-4 overflow-y-auto px-3.5 py-2 outline-none focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-inset sm:grid-cols-2"
      >
        {items.map((item) => (
          <li key={item.id} className="flex min-h-8 min-w-0 items-center text-sm">
            <span className="min-w-0 truncate">
              {item.name}
              {item.detail !== undefined && <span className="text-muted"> · {item.detail}</span>}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}
