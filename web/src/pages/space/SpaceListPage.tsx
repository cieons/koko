import { useId, useState } from "react";

import { SpaceAdd, SpaceList } from "../../api/space";
import { OwnerOnly } from "../../shell/caller";
import { useRead, useWrite } from "../../shell/data";
import { ErrorMessage, useT } from "../../shell/i18n";
import { Link } from "../../shell/router";

/**
 * SpaceListPage lists the Spaces the caller can act in, each linking to its
 * page; the Owner adds a shared Space here.
 */
export function SpaceListPage() {
  const t = useT();
  const titleId = useId();
  const list = useRead(SpaceList, {});
  return (
    <>
      <h1 id={titleId}>{t("space.list.title")}</h1>
      {list.error !== undefined && <ErrorMessage error={list.error} />}
      {list.data && (
        <ul aria-labelledby={titleId}>
          {list.data.spaces.map((space) => (
            <li key={space.id}>
              <Link to={spacePath(space.name)}>{space.name}</Link>
              {space.personal && <span className="tag"> {t("space.personal")}</span>}
            </li>
          ))}
        </ul>
      )}
      <OwnerOnly>
        <AddSpace onAdded={list.reload} />
      </OwnerOnly>
    </>
  );
}

/** AddSpace is the Owner's form for a new shared Space. A reversible write, it never asks. */
function AddSpace({ onAdded }: { onAdded: () => void }) {
  const t = useT();
  const [name, setName] = useState("");
  const add = useWrite(SpaceAdd);
  return (
    <form
      className="inline-form"
      onSubmit={(e) => {
        e.preventDefault();
        void add.submit({ name }).then((added) => {
          if (added) {
            setName("");
            onAdded();
          }
        });
      }}
    >
      <label>
        {t("space.add.name")} <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" />
      </label>{" "}
      <button type="submit" disabled={name === "" || add.pending}>
        {t("space.add.submit")}
      </button>
      {add.error !== undefined && <ErrorMessage error={add.error} />}
    </form>
  );
}

/** spacePath is the path of a Space's page. */
export function spacePath(name: string): string {
  return `/spaces/${encodeURIComponent(name)}`;
}
