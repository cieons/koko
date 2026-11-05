import { SpaceList } from "../../api/space";
import { useRead } from "../../shell/data";
import { ErrorMessage, useT } from "../../shell/i18n";

/** SpaceListPage lists the Spaces the caller can act in. */
export function SpaceListPage() {
  const t = useT();
  const list = useRead(SpaceList, {});
  return (
    <>
      <h1>{t("space.list.title")}</h1>
      {list.error !== undefined && <ErrorMessage error={list.error} />}
      {list.data && (
        <ul>
          {list.data.spaces.map((space) => (
            <li key={space.id}>{space.name}</li>
          ))}
        </ul>
      )}
    </>
  );
}
