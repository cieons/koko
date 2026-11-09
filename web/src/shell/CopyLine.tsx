import { Code } from "@astryxdesign/core/Code";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { useState } from "react";

import { useT } from "./i18n";

/**
 * CopyLine shows a line to be pasted elsewhere, such as a command or a code,
 * in Astryx's Code, which breaks anywhere on a phone and one click selects
 * whole, with a button that copies it and says whether the browser did. A
 * browser without the clipboard API, or one that refuses it, still shows
 * the line to copy by hand.
 */
export function CopyLine({ line }: { line: string }) {
  const t = useT();
  const [copied, setCopied] = useState<boolean>();
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(line);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };
  return (
    <VStack gap={3}>
      <HStack gap={2} vAlign="center">
        <Code className="koko-shown-once-line">{line}</Code>
        <IconButton
          label={t("shell.shownOnce.copy")}
          tooltip={t("shell.shownOnce.copy")}
          icon={<Icon icon={copied ? "checkDouble" : "copy"} />}
          variant="ghost"
          onClick={() => void copy()}
        />
      </HStack>
      {copied !== undefined && (
        <Text type="body" color="secondary" role="status">
          {copied ? t("shell.shownOnce.copied") : t("shell.shownOnce.copyFailed")}
        </Text>
      )}
    </VStack>
  );
}
