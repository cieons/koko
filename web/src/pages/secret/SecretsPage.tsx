import { Button, SearchField } from "@heroui/react";
import { Plus } from "lucide-react";
import { useId, useState } from "react";

import { type Secret, SecretAdd, SecretList } from "../../api/secret";
import { type Space, SpaceList } from "../../api/space";
import { useCaller } from "../../shell/caller";
import { useRead, useWrite } from "../../shell/data";
import { useT } from "../../shell/i18n";
import { type RememberedList, ResetView, useRememberedView } from "../../shell/rememberedView";
import { Field, SearchChoice } from "../../ui/fields";
import { FormDialog } from "../../ui/FormDialog";
import { notify } from "../../ui/notify";
import { Page, Sheet } from "../../ui/Page";
import { EmptyState, LoadFailed, Loading, NoMatch } from "../../ui/states";
import { qualified, SecretRow, useSpaceOptions, ValueField } from "./SecretRow";
import { encodeValue } from "./value";

/** allSpaces is the Space select's first item, "All Spaces": no Space is named so. */
const allSpaces = "*";

/**
 * SecretsPage lists the Secrets of every Space the caller can act in, as
 * `koko secret ls` does, one panel per Space, and never a value until one
 * is asked for (docs/web-design.md, "Secrets"). The search (q) and the
 * Space select (space) live in the URL, refined in place, so a bookmark
 * opens the page narrowed, and the browser remembers them
 * (useRememberedView), with Reset view. Each row copies, shows and
 * changes its Secret (SecretRow); Add Secret adds one.
 */
export function SecretsPage() {
  const t = useT();
  const list = useRead(SecretList, {});
  const spaces = useRead(SpaceList, {});
  const view = useRememberedView(secretsList);
  const { params, setParams } = view;
  const [adding, setAdding] = useState(false);
  const query = params.get("q") ?? "";
  const space = params.get("space") ?? "";

  // Each keystroke and choice refines the view in place rather than adding a visit.
  const refine = (changes: Readonly<Partial<Record<"q" | "space", string>>>) => {
    const next = new URLSearchParams(params);
    for (const [key, value] of Object.entries(changes)) {
      if (value === "") {
        next.delete(key);
      } else {
        next.set(key, value);
      }
    }
    setParams(next, { replace: true });
  };

  const add = (
    <Button onPress={() => setAdding(true)}>
      <Plus aria-hidden className="size-4" />
      {t("secret.add")}
    </Button>
  );
  const secrets = list.data?.secrets;
  // With no Secret yet, the empty state offers Add Secret, once.
  return (
    <Page title={t("secret.title")} lead={t("secret.lead")} actions={secrets?.length === 0 ? undefined : add}>
      {list.error !== undefined && (
        <Sheet list className="mb-6">
          <LoadFailed error={list.error} retry={list.reload} />
        </Sheet>
      )}
      {secrets === undefined ? (
        list.error === undefined && (
          <Sheet list>
            <Loading />
          </Sheet>
        )
      ) : secrets.length === 0 ? (
        <EmptyState title={t("secret.empty.title")} lead={t("secret.empty.lead")} action={add} />
      ) : (
        <>
          <div className="mb-5 flex flex-wrap items-center gap-2.5">
            <SearchField
              aria-label={t("secret.search")}
              value={query}
              onChange={(q) => refine({ q })}
              className="min-w-56 flex-1"
              fullWidth
            >
              <SearchField.Group>
                <SearchField.SearchIcon />
                <SearchField.Input placeholder={t("secret.search")} autoComplete="off" spellCheck="false" />
                <SearchField.ClearButton aria-label={t("secret.clearSearch")} />
              </SearchField.Group>
            </SearchField>
            {(spaces.data?.spaces.length ?? 0) > 1 && (
              <SpaceSelect
                spaces={spaces.data?.spaces ?? []}
                value={space}
                onChange={(chosen) => refine({ space: chosen })}
              />
            )}
            <ResetView view={view} />
          </div>
          <Listing
            secrets={secrets}
            query={query}
            space={space}
            spaces={spaces.data?.spaces}
            onChanged={list.reload}
            onClear={() => refine({ q: "", space: "" })}
          />
        </>
      )}
      {adding && (
        <AddSecret
          spaces={spaces.data?.spaces}
          spacesError={spaces.error}
          space={space}
          onAdded={(name) => {
            setAdding(false);
            notify(t("secret.added", { name }));
            list.reload();
          }}
          onCancel={() => setAdding(false)}
        />
      )}
    </Page>
  );
}

/** secretsList is the list the Secrets page's view is of, whose default view has no query. */
function secretsList(): RememberedList {
  return { list: "secrets", base: "" };
}

/** SpaceSelect narrows the page to one Space, "All Spaces" first: a searchable choice, since Spaces can grow. */
function SpaceSelect({
  spaces,
  value,
  onChange,
}: {
  spaces: readonly Space[];
  value: string;
  onChange: (space: string) => void;
}) {
  const t = useT();
  const options = [{ id: allSpaces, label: t("secret.allSpaces") }, ...useSpaceOptions(spaces)];
  return (
    <SearchChoice
      label={t("secret.space")}
      hideLabel
      value={value === "" ? allSpaces : value}
      onChange={(chosen) => chosen !== null && onChange(chosen === allSpaces ? "" : chosen)}
      options={options}
      className="w-full sm:w-56"
    />
  );
}

/**
 * Listing is the Secrets the search and the Space select leave, one panel
 * per Space, in the order secret.list answers them: by Space, then name.
 * The search matches <space>/<name>, whatever its case.
 */
function Listing({
  secrets,
  query,
  space,
  spaces,
  onChanged,
  onClear,
}: {
  secrets: readonly Secret[];
  query: string;
  space: string;
  spaces: readonly Space[] | undefined;
  onChanged: () => void;
  onClear: () => void;
}) {
  const t = useT();
  const term = query.trim().toLowerCase();
  const shown = secrets.filter(
    (s) => (space === "" || s.space === space) && qualified(s).toLowerCase().includes(term),
  );
  if (shown.length === 0) {
    return <NoMatch message={t("secret.noMatch")} clear={t("secret.clear")} onClear={onClear} />;
  }
  const groups = new Map<string, Secret[]>();
  for (const s of shown) {
    groups.set(s.space, [...(groups.get(s.space) ?? []), s]);
  }
  return (
    <div className="flex flex-col gap-6">
      {[...groups].map(([name, items]) => (
        <SpacePanel
          key={name}
          name={name}
          personal={spaces?.find((s) => s.name === name)?.personal === true}
          secrets={items}
          spaces={spaces}
          onChanged={onChanged}
        />
      ))}
    </div>
  );
}

/** SpacePanel is one Space's Secrets: a heading with its name and count, over a panel of rows. */
function SpacePanel({
  name,
  personal,
  secrets,
  spaces,
  onChanged,
}: {
  name: string;
  personal: boolean;
  secrets: readonly Secret[];
  spaces: readonly Space[] | undefined;
  onChanged: () => void;
}) {
  const t = useT();
  const id = useId();
  const count = t(secrets.length === 1 ? "secret.count.one" : "secret.count.other", { count: secrets.length });
  return (
    <section aria-labelledby={id}>
      <h2 className="t-heading mb-2 flex flex-wrap items-baseline gap-x-2 px-1">
        <span id={id}>{name}</span>
        <span className="t-label font-normal">{personal ? t("secret.personalCount", { count }) : count}</span>
      </h2>
      <ul className="sheet overflow-hidden">
        {secrets.map((s) => (
          <SecretRow key={s.id} secret={s} spaces={spaces} onChanged={onChanged} />
        ))}
      </ul>
    </section>
  );
}

/**
 * AddSecret adds a Secret, its value typed in, to a Space the caller can
 * act in: the Space the page is narrowed to, or else their Personal Space,
 * unless they choose another. The value goes as the base64 of its UTF-8. It
 * never asks.
 */
function AddSecret({
  spaces,
  spacesError,
  space: narrowed,
  onAdded,
  onCancel,
}: {
  spaces: readonly Space[] | undefined;
  spacesError: unknown;
  space: string;
  onAdded: (name: string) => void;
  onCancel: () => void;
}) {
  const t = useT();
  const caller = useCaller();
  const [opened] = useState(narrowed === "" ? caller.user.name : narrowed);
  const [space, setSpace] = useState<string | null>(opened);
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const add = useWrite(SecretAdd);
  const options = useSpaceOptions(spaces ?? []);
  return (
    <FormDialog
      title={t("secret.add")}
      lead={t("secret.add.lead")}
      submit={t("secret.add")}
      size="md"
      pending={add.pending}
      changed={space !== opened || name !== "" || value !== ""}
      error={add.error ?? spacesError}
      onSubmit={() =>
        void add
          .submit({ space: space ?? "", name, value_base64: encodeValue(value) })
          .then((added) => added && onAdded(qualified(added.secret)))
      }
      onClose={onCancel}
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field
          label={t("secret.name")}
          value={name}
          onChange={setName}
          code
          autoFocus
          validate={(v) => (v === "" ? t("secret.nameRequired") : undefined)}
        />
        <SearchChoice
          label={t("secret.space")}
          value={space}
          onChange={setSpace}
          options={options}
          validate={(v) => (v === null ? t("secret.spaceRequired") : undefined)}
        />
      </div>
      <ValueField label={t("secret.value")} value={value} onChange={setValue} autoFocus={false} />
    </FormDialog>
  );
}
