import { FolderInput, Trash2 } from "lucide-react";
import { type ReactNode, useState } from "react";

import type { Op } from "../../api/api";
import type { SecretRef } from "../../api/enroll";
import { type Secret, SecretMove, SecretRemove } from "../../api/secret";
import type { Space } from "../../api/space";
import { useWrite } from "../../shell/data";
import { useCount, useT } from "../../shell/i18n";
import { SearchChoice } from "../../ui/fields";
import { BatchDialog, type BatchWrite, type Item } from "../batch/BatchDialog";
import { type SelectMode, useSelectMode } from "../batch/useSelectMode";
import { wordsOf } from "../batch/words";
import { qualified, useSpaceOptions } from "./SecretRow";

// The Secrets page's Select mode (#134): Move and Delete, each one call of
// its operation's batch form, applied to every Secret or to none (ADR 0050).

/** SecretAction is an action a batch of Secrets takes. */
type SecretAction = "move" | "delete";

/** secretItem is a Secret as a batch's dialog lists it: <space>/<secret>. */
function secretItem(secret: Secret): Item<Secret> {
  return { id: secret.id, name: qualified(secret), record: secret };
}

/**
 * useSecretsWrite is the batch form of a Secret write: op for the Secrets
 * given, each by its Space and name, with what the rest of its request
 * holds.
 */
function useSecretsWrite<Req extends { secrets?: SecretRef[] }>(
  op: Op<Req, unknown>,
  rest: () => Omit<Req, "space" | "name" | "secrets">,
): BatchWrite<Secret> {
  const write = useWrite(op);
  return {
    send: async (secrets) =>
      (await write.submit({ ...rest(), secrets: secrets.map(({ space, name }) => ({ space, name })) } as Req)) !==
      undefined,
    pending: write.pending,
    error: write.error,
  };
}

/** useSecretSelectMode holds the Secrets page's Select mode (useSelectMode) and its actions: Move and Delete. */
export function useSecretSelectMode({
  secrets,
  spaces,
}: {
  secrets: readonly Secret[];
  spaces: readonly Space[];
}): { mode: SelectMode; select: ReactNode; overlays: ReactNode } {
  const t = useT();
  return useSelectMode<Secret, SecretAction>({
    records: secrets,
    idOf: (s) => s.id,
    barLabel: t("batch.secret.bar"),
    actions: [
      { id: "move", label: t("batch.action.move"), icon: FolderInput },
      { id: "delete", label: t("secret.delete"), icon: Trash2, danger: true },
    ],
    dialog: (action, selected, onClose) =>
      action === "move" ? (
        <MoveSecrets secrets={selected} spaces={spaces} onClose={onClose} />
      ) : (
        <DeleteSecrets secrets={selected} onClose={onClose} />
      ),
  });
}

/** DialogProps are what an action's dialog gets. */
interface DialogProps {
  /** secrets are the Secrets selected. */
  secrets: readonly Secret[];
  onClose: (applied: boolean) => void;
}

/** useThings counts Secrets in words: "1 Secret", "5 Secrets". */
function useThings(): (n: number) => string {
  const t = useT();
  return (n) => t(n === 1 ? "secret.count.one" : "secret.count.other", { count: n });
}

/** MoveSecrets moves Secrets, each under its name, to one Space; those in it already are left out. */
function MoveSecrets({ secrets, spaces, onClose }: DialogProps & { spaces: readonly Space[] }) {
  const t = useT();
  const count = useCount();
  const things = useThings();
  const [space, setSpace] = useState<string | null>(null);
  // A Space that holds every one of them already is no move.
  const options = useSpaceOptions(spaces.filter((s) => secrets.some((secret) => secret.space !== s.name)));
  const moving = space === null ? secrets : secrets.filter((s) => s.space !== space);
  const staying = secrets.length - moving.length;
  const to = space ?? "";
  const write = useSecretsWrite(SecretMove, () => ({ new_space: to }));
  const title = (n: number) => t("batch.secret.move.title", { things: things(n) });
  return (
    <BatchDialog
      title={title}
      lead={t("batch.secret.move.lead")}
      kind={t("secret.title")}
      items={moving.map(secretItem)}
      leftOut={staying > 0 ? count("batch.secret.move.staying", staying, { space: to }) : undefined}
      submit={title}
      write={write}
      words={wordsOf(t, count, "batch.secret.move", { space: to })}
      changed={space !== null}
      onClose={onClose}
    >
      <SearchChoice
        label={t("secret.moveTo")}
        value={space}
        onChange={setSpace}
        options={options}
        validate={(v) => (v === null ? t("secret.spaceRequired") : undefined)}
      />
    </BatchDialog>
  );
}

/** DeleteSecrets asks once for them all, and, for more than one, for their number typed, then deletes each Secret and its value for good. */
function DeleteSecrets({ secrets, onClose }: DialogProps) {
  const t = useT();
  const count = useCount();
  const things = useThings();
  const write = useSecretsWrite(SecretRemove, () => ({}));
  const title = (n: number) => t("batch.secret.remove.title", { things: things(n) });
  return (
    <BatchDialog
      danger
      title={title}
      lead={t("batch.secret.remove.lead")}
      kind={t("secret.title")}
      items={secrets.map(secretItem)}
      typed={secrets.length > 1}
      submit={title}
      write={write}
      words={wordsOf(t, count, "batch.secret.remove")}
      onClose={onClose}
    />
  );
}
