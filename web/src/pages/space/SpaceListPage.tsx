import { Badge } from "@astryxdesign/core/Badge";
import { Button } from "@astryxdesign/core/Button";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, Layout, LayoutContent, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Table, type TableColumn, useTableStickyColumns } from "@astryxdesign/core/Table";
import { Heading } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { Plus } from "lucide-react";
import { useState } from "react";

import { type Space, SpaceAdd, SpaceList } from "../../api/space";
import { FormDialog } from "../../shell/AdaptiveDialog";
import { OwnerOnly } from "../../shell/caller";
import { useRead, useWrite } from "../../shell/data";
import { ErrorMessage, useT } from "../../shell/i18n";
import { Link } from "../../shell/router";
import { spacePath } from "./path";

/** SpaceRow is a Space as a row of the table. */
interface SpaceRow extends Record<string, unknown> {
  id: string;
  space: Space;
}

/**
 * SpaceListPage lists the Spaces the caller can act in, each linking to its
 * page, a Personal Space marked so; the Owner adds a shared Space here.
 */
export function SpaceListPage() {
  const t = useT();
  const list = useRead(SpaceList, {});
  const [adding, setAdding] = useState(false);
  const spaces = list.data?.spaces ?? [];
  // On a phone the table scrolls sideways in its own box, its name in view.
  const sticky = useTableStickyColumns<SpaceRow>({ startKeys: ["name"] });
  const columns: TableColumn<SpaceRow>[] = [
    {
      key: "name",
      header: t("space.column.name"),
      renderCell: ({ space }) => <Link to={spacePath(space.name)}>{space.name}</Link>,
    },
    {
      key: "kind",
      header: t("space.column.kind"),
      renderCell: ({ space }) => (space.personal ? t("space.personal") : t("space.shared")),
    },
  ];
  return (
    <Layout
      contentWidth={1080}
      content={
        <LayoutContent>
          <VStack gap={6}>
            <HStack gap={3} vAlign="center" wrap="wrap">
              <StackItem size="fill">
                <HStack gap={2} vAlign="center">
                  <Heading level={1}>{t("space.list.title")}</Heading>
                  {list.data && <Badge variant="neutral" label={String(spaces.length)} />}
                </HStack>
              </StackItem>
              <OwnerOnly>
                <Button
                  variant="primary"
                  label={t("space.add.button")}
                  icon={<Icon icon={Plus} size="sm" />}
                  onClick={() => setAdding(true)}
                />
              </OwnerOnly>
            </HStack>
            {list.error !== undefined && <ErrorMessage error={list.error} />}
            {list.data && (
              <Table<SpaceRow>
                aria-label={t("space.list.title")}
                data={spaces.map((space) => ({ id: space.id, space }))}
                columns={columns}
                idKey="id"
                plugins={{ sticky }}
                hasHover
                textOverflow="wrap"
              />
            )}
          </VStack>
          {adding && (
            <AddSpace
              onAdded={() => {
                setAdding(false);
                list.reload();
              }}
              onCancel={() => setAdding(false)}
            />
          )}
        </LayoutContent>
      }
    />
  );
}

/** AddSpace is the Owner's dialog for a new shared Space. A reversible write, it never asks. */
function AddSpace({ onAdded, onCancel }: { onAdded: () => void; onCancel: () => void }) {
  const t = useT();
  const [name, setName] = useState("");
  const add = useWrite(SpaceAdd);
  return (
    <FormDialog
      title={t("space.add.title")}
      submit={t("space.add.submit")}
      ready={name !== "" && !add.pending}
      error={add.error}
      onSubmit={() => void add.submit({ name }).then((added) => added && onAdded())}
      onClose={onCancel}
    >
      <TextInput
        label={t("space.add.name")}
        value={name}
        onChange={setName}
        autoComplete="off"
        autoCapitalize="off"
        hasAutoCorrect={false}
        hasSpellCheck={false}
      />
    </FormDialog>
  );
}
