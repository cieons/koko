import { Badge } from "@astryxdesign/core/Badge";
import { Button } from "@astryxdesign/core/Button";
import { Divider } from "@astryxdesign/core/Divider";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, LayoutHeader, Section, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Heading, Text } from "@astryxdesign/core/Text";
import { ArrowLeft, ExternalLink, X } from "lucide-react";
import type { ReactNode } from "react";

import type { Host } from "../../api/host";
import { useT } from "../../shell/i18n";

// What a Host's and a Service's page and the lists' detail panes show of a
// record: its fields and Attributes by key, as stored and never
// translated, its Note as written, and a Host's state.

/** narrow is the width at and below which a record page's rail moves under its content, detail-page's. */
export const narrow = "(max-width: 1024px)";

/** states are the Badge each state of a Host shows in: a hue, not a severity. */
const states = { registered: "blue", managed: "green", retired: "neutral" } as const;

/** StateBadge is a Host's state, as stored, in a Badge of its hue. */
export function StateBadge({ state }: { state: Host["state"] }) {
  return <Badge variant={states[state as keyof typeof states] ?? "neutral"} label={state} />;
}

/** FieldList shows a record's fields by key, in the order given. */
export function FieldList({ label, fields }: { label: string; fields: readonly (readonly [string, ReactNode])[] }) {
  return (
    <MetadataList title={<Heading level={2}>{label}</Heading>} label={{ position: "start", width: 112 }}>
      {fields.map(([key, value]) => (
        <MetadataListItem key={key} label={key}>
          {value}
        </MetadataListItem>
      ))}
    </MetadataList>
  );
}

/** Attributes shows a record's Attributes by key, in key order, or that it has none. */
export function Attributes({ attributes }: { attributes: Readonly<Record<string, string | number>> }) {
  const t = useT();
  const keys = Object.keys(attributes).sort();
  return keys.length === 0 ? (
    <VStack gap={2}>
      <Heading level={2}>{t("host.attributes")}</Heading>
      <Text type="body" color="secondary">
        {t("host.attributes.none")}
      </Text>
    </VStack>
  ) : (
    <FieldList label={t("host.attributes")} fields={keys.map((key) => [key, String(attributes[key])] as const)} />
  );
}

/** Note shows a record's Note as written, its lines kept, or that it has none. */
export function Note({ note }: { note: string | undefined }) {
  const t = useT();
  return (
    <VStack gap={2}>
      <Heading level={2}>{t("host.note")}</Heading>
      {note === undefined ? (
        <Text type="body" color="secondary">
          {t("host.note.none")}
        </Text>
      ) : (
        <Text type="body" className="koko-note">
          {note}
        </Text>
      )}
    </VStack>
  );
}

/**
 * DetailPane is the body of a list's detail pane: the record's name and
 * what says where it is, a button that opens its page, one that closes the
 * pane, and the sections given, divided.
 */
export function DetailPane({
  name,
  badge,
  about,
  href,
  onClose,
  sections,
}: {
  name: string;
  badge?: ReactNode;
  about: string;
  href: string;
  onClose: () => void;
  /** sections are what the pane shows of the record, in order, each divided from the one before. */
  sections: readonly ReactNode[];
}) {
  const t = useT();
  return (
    <VStack gap={0}>
      <Section variant="transparent" padding={4}>
        <VStack gap={4}>
          <HStack gap={2}>
            <StackItem size="fill">
              <VStack gap={2}>
                <HStack gap={2} vAlign="center" wrap="wrap">
                  {badge}
                  <Text type="supporting" color="secondary">
                    {about}
                  </Text>
                </HStack>
                <Heading level={2}>{name}</Heading>
              </VStack>
            </StackItem>
            <IconButton
              label={t("host.detail.close")}
              variant="ghost"
              size="sm"
              icon={<Icon icon={X} size="sm" />}
              onClick={onClose}
            />
          </HStack>
          <Button label={t("host.detail.open")} width="100%" href={href} icon={<Icon icon={ExternalLink} size="sm" />} />
        </VStack>
      </Section>
      {sections.map((section, i) => (
        <VStack key={i} gap={0}>
          <Divider />
          <Section variant="transparent" padding={4}>
            {section}
          </Section>
        </VStack>
      ))}
    </VStack>
  );
}

/**
 * RecordHeader is a record page's summary header, detail-page's: a link
 * back to its list, its name, what says what it is, its actions, and any
 * refusal under them.
 */
export function RecordHeader({
  back,
  name,
  about,
  actions,
  children,
}: {
  back: { href: string; label: string };
  name: string;
  about: ReactNode[];
  actions: ReactNode[];
  children?: ReactNode;
}) {
  return (
    <LayoutHeader hasDivider padding={4}>
      <VStack gap={3}>
        <HStack gap={4} vAlign="start" wrap="wrap">
          <StackItem size="fill">
            <VStack gap={1}>
              <Link href={back.href} color="secondary">
                <HStack gap={1} vAlign="center">
                  <Icon icon={ArrowLeft} size="sm" color="inherit" />
                  {back.label}
                </HStack>
              </Link>
              <Heading level={1} className="koko-record-name">
                {name}
              </Heading>
              <HStack gap={2} vAlign="center" wrap="wrap">
                {about}
              </HStack>
            </VStack>
          </StackItem>
          <HStack gap={2} wrap="wrap">
            {actions}
          </HStack>
        </HStack>
        {children}
      </VStack>
    </LayoutHeader>
  );
}
